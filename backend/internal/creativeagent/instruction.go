package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ErrUnsupportedSegment means a submission named something this deployment
// cannot execute — a skill whose tools are not registered, or a content kind
// the model link cannot carry. It is deliberately not ErrValidation: the
// request is well formed and the photographer can act on the difference by
// choosing something else, so answering "malformed" would be a lie.
var ErrUnsupportedSegment = errors.New("creative instruction segment not supported here")

const (
	// maxBlocks bounds both a submission's segments and a stored message's
	// blocks. One number, because the body is the submission's projection: a
	// submission that parses has to be renderable.
	maxBlocks = 200
	// maxSkillRefs is 1 in this phase. Two skills in one submission is refused
	// rather than concatenated — stitching two sets of instructions together
	// produces a third set that neither author wrote.
	maxSkillRefs = 1
	// maxSceneRefs bounds the distinct scenes one submission references. It
	// is a shape like maxSceneSelection, not a configurable budget: each
	// scene is read inside the run transaction, and five is the most a
	// sentence can sensibly compare.
	maxSceneRefs = 5
)

// InstructionSegment is one ordered piece of what a photographer submitted. It
// is a discriminated union carried as a flat struct, matching Block: each type
// owns its own fields and naming a foreign one is refused rather than ignored,
// so a client that mislabels a segment hears about it instead of silently
// losing the payload.
//
// The reference kinds carry ids and nothing else. A submission locates content
// it already owns; it never ships a copy, and an id on its own grants no
// reading rights — every one of them is re-checked below.
type InstructionSegment struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// SkillID and SkillVersionID always travel together. The version alone
	// would make the server pick the skill, which is how a version belonging to
	// a different skill gets read as if it were this one.
	SkillID           string `json:"skill_id,omitempty"`
	SkillVersionID    string `json:"skill_version_id,omitempty"`
	ContentRevisionID string `json:"content_revision_id,omitempty"`
	// Command asks the assistant to act on the conversation itself. Text is
	// then the command's optional argument, never words for the model.
	Command string `json:"command,omitempty"`
	// DocumentID and ObjectIDs belong to scene_ref: a scene, and optionally
	// the objects in it, referenced at this position. Omitted ObjectIDs mean
	// the whole scene; an empty list is a client that lost its selection.
	DocumentID string   `json:"document_id,omitempty"`
	ObjectIDs  []string `json:"object_ids,omitempty"`
}

// UnmarshalJSON checks when a scene field was sent, including zero values.
func (s *InstructionSegment) UnmarshalJSON(raw []byte) error {
	type segment InstructionSegment
	var next segment
	if err := creativeops.Decode(raw, &next); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for field := range fields {
		switch {
		case strings.EqualFold(field, "object_ids"):
			if next.Type != "scene_ref" || next.ObjectIDs == nil {
				return creativeops.ErrValidation
			}
		case strings.EqualFold(field, "document_id"):
			if next.Type != "scene_ref" {
				return creativeops.ErrValidation
			}
		default:
			if next.Type == "scene_ref" && (strings.EqualFold(field, "text") ||
				strings.EqualFold(field, "skill_id") || strings.EqualFold(field, "skill_version_id") ||
				strings.EqualFold(field, "content_revision_id") || strings.EqualFold(field, "command")) {
				return creativeops.ErrValidation
			}
		}
	}
	*s = InstructionSegment(next)
	return nil
}

// ResolvedSkill is the one skill a submission fixed, with the position it was
// named at. The ordinal is what the message registers, so a later reader can
// say which segment the reference belonged to rather than only that one
// existed.
type ResolvedSkill struct {
	SegmentOrdinal int
	Snapshot       creativeskill.Snapshot
}

// ResolvedInstruction is what the resolver hands back: an ordered display
// projection, the authorised and fixed skill, and the content this submission
// must keep readable. It is deliberately not one concatenated string — the
// message has to render the pieces, and the retention roots have to be rows.
type ResolvedInstruction struct {
	Body Body
	// Skill is nil when the submission named none, which is the ordinary case
	// for plain conversation.
	Skill *ResolvedSkill
	// ContentRefs are the revisions the message will keep readable after the
	// run payload expires. Ids buried in the body are not roots.
	ContentRefs []ContentRef
	// Bytes is what the projection actually costs against the body budget,
	// reported rather than recomputed by every caller.
	Bytes int
	// Command is set when the submission is a command, and CommandArgs is its
	// argument; a command submission has no other segment.
	Command     string
	CommandArgs string
}

// ResolveInstruction turns a submission into something that can be stored and
// executed: every reference is re-derived from the database under this
// account's scope, and nothing the client sent is carried through as authority.
//
// It runs before the write, not inside it. A version's content is immutable so
// reading it here is sound, but a skill can be switched off and a content grant
// can be revoked between here and dispatch — milestone B re-checks both inside
// the run transaction. What this answers is "may this be submitted at all".
func (s *Service) ResolveInstruction(ctx context.Context, scope store.AccountScope, segments []InstructionSegment) (ResolvedInstruction, error) {
	if err := validSegments(segments); err != nil {
		return ResolvedInstruction{}, err
	}
	out := ResolvedInstruction{Body: Body{Blocks: make([]Block, 0, len(segments))}}
	for ordinal, segment := range segments {
		switch segment.Type {
		case "text":
			out.Body.Blocks = append(out.Body.Blocks, Block{Type: "text", Text: segment.Text})
		case "skill_ref":
			block, err := s.resolveSkillSegment(ctx, scope, ordinal, segment, &out)
			if err != nil {
				return ResolvedInstruction{}, err
			}
			out.Body.Blocks = append(out.Body.Blocks, block)
		case "content_ref":
			if err := s.resolveContentSegment(ctx, scope, segment, &out); err != nil {
				return ResolvedInstruction{}, err
			}
			out.Body.Blocks = append(out.Body.Blocks, Block{Type: "content_ref", RefID: segment.ContentRevisionID})
		case "command":
			out.Command, out.CommandArgs = segment.Command, strings.TrimSpace(segment.Text)
			out.Body.Blocks = append(out.Body.Blocks, Block{Type: "command", Command: segment.Command, Text: out.CommandArgs})
		case "scene_ref":
			// A placeholder: the title, revision and names are read inside the
			// run transaction, where the snapshot and the run inputs share one
			// read. Until then the block costs its lower bound.
			scene := &BlockScene{DocumentID: segment.DocumentID, Objects: make([]BlockSceneObject, 0, len(segment.ObjectIDs))}
			for _, id := range segment.ObjectIDs {
				scene.Objects = append(scene.Objects, BlockSceneObject{ID: id})
			}
			out.Body.Blocks = append(out.Body.Blocks, Block{Type: "scene_ref", Scene: scene})
		}
	}
	// The projection is checked against the same rule that will store it, so a
	// submission that cannot become a message is refused now rather than after
	// every reference has been resolved and charged for.
	if err := validBody(out.Body, s.limits); err != nil {
		return ResolvedInstruction{}, err
	}
	out.Bytes = bodyBytes(out.Body)
	return out, nil
}

// validSegments decides everything about the shape of a submission that does
// not need the database, so a malformed request never reaches a lookup.
func validSegments(segments []InstructionSegment) error {
	if len(segments) == 0 {
		return creativeops.ErrValidation
	}
	if len(segments) > maxBlocks {
		return ErrLimit
	}
	skills := 0
	var scenes []string
	for _, segment := range segments {
		if segment.Command != "" && segment.Type != "command" ||
			(segment.DocumentID != "" || segment.ObjectIDs != nil) && segment.Type != "scene_ref" {
			return creativeops.ErrValidation
		}
		switch segment.Type {
		case "text":
			if segment.Text == "" || !utf8.ValidString(segment.Text) ||
				segment.SkillID != "" || segment.SkillVersionID != "" || segment.ContentRevisionID != "" {
				return creativeops.ErrValidation
			}
		case "skill_ref":
			if segment.SkillID == "" || segment.SkillVersionID == "" ||
				segment.Text != "" || segment.ContentRevisionID != "" {
				return creativeops.ErrValidation
			}
			skills++
		case "content_ref":
			if segment.ContentRevisionID == "" ||
				segment.Text != "" || segment.SkillID != "" || segment.SkillVersionID != "" {
				return creativeops.ErrValidation
			}
		case "command":
			// A command stands alone: mixing it with words would leave the
			// model a request nobody can tell apart from the command.
			if len(segments) != 1 || segment.Command != compactCommand || !utf8.ValidString(segment.Text) ||
				utf8.RuneCountInString(strings.TrimSpace(segment.Text)) > compactFocusRunes ||
				segment.SkillID != "" || segment.SkillVersionID != "" || segment.ContentRevisionID != "" {
				return creativeops.ErrValidation
			}
		case "scene_ref":
			if !validSceneRefSegment(segment) {
				return creativeops.ErrValidation
			}
			if !slices.Contains(scenes, segment.DocumentID) {
				scenes = append(scenes, segment.DocumentID)
			}
		default:
			// An unknown type is refused, never skipped. Dropping it would send
			// the model a different instruction from the one submitted.
			return creativeops.ErrValidation
		}
	}
	// More than one skill is malformed rather than oversized: the fix is to
	// choose one, not to send less. A 413 would tell the client to shorten
	// something that is not too long.
	if skills > maxSkillRefs {
		return creativeops.ErrValidation
	}
	// Too many scenes is a size, unlike two skills: the same words still
	// make sense split over two messages.
	if len(scenes) > maxSceneRefs {
		return ErrLimit
	}
	return nil
}

// validSceneRefSegment checks a scene reference's own fields. The object
// bound is per reference, so one scene can be referenced twice with different
// objects each time.
func validSceneRefSegment(segment InstructionSegment) bool {
	if segment.DocumentID == "" || !utf8.ValidString(segment.DocumentID) || segment.Text != "" ||
		segment.SkillID != "" || segment.SkillVersionID != "" || segment.ContentRevisionID != "" {
		return false
	}
	if segment.ObjectIDs != nil && len(segment.ObjectIDs) == 0 || len(segment.ObjectIDs) > maxSceneSelection {
		return false
	}
	for i, id := range segment.ObjectIDs {
		if id == "" || !utf8.ValidString(id) || slices.Contains(segment.ObjectIDs[:i], id) {
			return false
		}
	}
	return true
}

func (s *Service) resolveSkillSegment(ctx context.Context, scope store.AccountScope,
	ordinal int, segment InstructionSegment, out *ResolvedInstruction) (Block, error) {
	// ResolveVersion checks that this version belongs to this skill and that
	// this account may see it; a mismatch is reported as absence, so a guessed
	// pairing learns nothing.
	snapshot, err := s.skills.ResolveVersion(ctx, scope, segment.SkillID, segment.SkillVersionID)
	if err != nil {
		return Block{}, err
	}
	if !snapshot.Usable() {
		// The author's own switch. A withdrawn version still renders in history
		// but cannot start new work.
		return Block{}, ErrUnsupportedSegment
	}
	if available, _ := runnableHere(s.registeredToolRefs(), snapshot.Manifest.Dependencies()); !available {
		return Block{}, ErrUnsupportedSegment
	}
	out.Skill = &ResolvedSkill{SegmentOrdinal: ordinal, Snapshot: snapshot}
	// Everything shown comes from the server's own read. The client sent two
	// ids and is told what they mean; it never supplies the name or the digest.
	return Block{Type: "skill_ref", Skill: &BlockSkill{
		SkillID:       snapshot.SkillID,
		VersionID:     snapshot.ID,
		VersionNumber: snapshot.VersionNumber,
		Digest:        snapshot.Digest,
		DisplayName:   snapshot.DisplayName,
	}}, nil
}

func (s *Service) resolveContentSegment(ctx context.Context, scope store.AccountScope,
	segment InstructionSegment, out *ResolvedInstruction) error {
	for _, earlier := range out.ContentRefs {
		// The same revision twice is one root, not two. The body still shows
		// both segments; only the retention row is deduplicated.
		if earlier.RevisionID == segment.ContentRevisionID {
			return nil
		}
	}
	if len(out.ContentRefs) >= s.limits.Attachments {
		return ErrLimit
	}
	var revision creativecontent.Revision
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		revision, err = creativecontent.RequireUsable(ctx, tx, segment.ContentRevisionID, "display")
		return err
	})
	switch {
	case errors.Is(err, creativecontent.ErrNotFound), errors.Is(err, creativecontent.ErrMissingRoot):
		// Out of this account's reach is reported as absence, the same answer a
		// revision that never existed gets.
		return ErrNotFound
	case errors.Is(err, creativecontent.ErrUsageDenied):
		return ErrUnsupportedSegment
	case err != nil:
		return err
	}
	// Content kind and model capability are separate checks; the latter is
	// validated when the run chooses a model and consent.
	if revision.Kind != "text" && revision.Kind != creativecontent.CharacterKind && (revision.Kind != "image" || s.images == nil) {
		return ErrUnsupportedSegment
	}
	out.ContentRefs = append(out.ContentRefs, ContentRef{RevisionID: segment.ContentRevisionID, Role: "input"})
	return nil
}
