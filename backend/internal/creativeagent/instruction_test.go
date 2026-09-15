package creativeagent

import (
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// A submission is a discriminated union, and every type owns its own fields.
// Naming a foreign one is refused rather than ignored: dropping it would send
// the model a different instruction from the one the photographer submitted.
func TestASegmentMayOnlyCarryItsOwnFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		segment InstructionSegment
	}{
		{"文字带 Skill 字段", InstructionSegment{Type: "text", Text: "x", SkillID: "ccsk_x"}},
		{"文字带内容字段", InstructionSegment{Type: "text", Text: "x", ContentRevisionID: "ccrv_x"}},
		{"文字为空", InstructionSegment{Type: "text"}},
		{"skill_ref 只给 Skill", InstructionSegment{Type: "skill_ref", SkillID: "ccsk_x"}},
		{"skill_ref 只给版本", InstructionSegment{Type: "skill_ref", SkillVersionID: "ccsv_x"}},
		{"skill_ref 夹带文字", InstructionSegment{Type: "skill_ref", SkillID: "ccsk_x", SkillVersionID: "ccsv_x", Text: "x"}},
		{"content_ref 为空", InstructionSegment{Type: "content_ref"}},
		{"content_ref 夹带 Skill", InstructionSegment{Type: "content_ref", ContentRevisionID: "ccrv_x", SkillID: "ccsk_x"}},
		{"未知类型", InstructionSegment{Type: "attachment_draft", Text: "x"}},
		{"类型为空", InstructionSegment{Text: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validSegments([]InstructionSegment{tc.segment}); !errors.Is(err, creativeops.ErrValidation) {
				t.Fatalf("%+v must be refused as malformed, got %v", tc.segment, err)
			}
		})
	}
}

// The two bounds of a submission are different failures. Too many segments is a
// size the client can fix by sending less; two skills is not — the fix is to
// choose one, and telling the client to shorten something would be advice that
// cannot work.
func TestTheTwoWaysASubmissionIsTooMuchAreDifferentAnswers(t *testing.T) {
	crowded := make([]InstructionSegment, maxBlocks+1)
	for i := range crowded {
		crowded[i] = InstructionSegment{Type: "text", Text: "x"}
	}
	if err := validSegments(crowded); !errors.Is(err, ErrLimit) {
		t.Fatalf("past the segment count must be a limit, got %v", err)
	}
	if err := validSegments(crowded[:maxBlocks]); err != nil {
		t.Fatalf("exactly the segment count must be accepted, got %v", err)
	}

	two := []InstructionSegment{
		{Type: "skill_ref", SkillID: "ccsk_a", SkillVersionID: "ccsv_a"},
		{Type: "skill_ref", SkillID: "ccsk_b", SkillVersionID: "ccsv_b"},
	}
	if err := validSegments(two); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("two skills must be refused as malformed, not as oversized: %v", err)
	}
	// The same skill twice is the same refusal. Nothing is concatenated and
	// nothing is silently deduplicated into one instruction.
	same := []InstructionSegment{
		{Type: "skill_ref", SkillID: "ccsk_a", SkillVersionID: "ccsv_a"},
		{Type: "skill_ref", SkillID: "ccsk_a", SkillVersionID: "ccsv_a"},
	}
	if err := validSegments(same); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("the same skill twice must be refused, got %v", err)
	}
	if err := validSegments(nil); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("an empty submission is nothing to do, got %v", err)
	}
}

// The resolver answers about references, so a submission of plain words must
// not need a skill or a content lookup to succeed.
func TestPlainWordsResolveWithoutAnyReference(t *testing.T) {
	f := setup(t)
	resolved, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "text", Text: "帮我把上周的片子理一理。"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Skill != nil || len(resolved.ContentRefs) != 0 {
		t.Fatalf("plain words acquired references: %+v", resolved)
	}
	if len(resolved.Body.Blocks) != 1 || resolved.Body.Blocks[0].Text != "帮我把上周的片子理一理。" {
		t.Fatalf("the display projection lost the words: %+v", resolved.Body)
	}
	if resolved.Bytes != len("帮我把上周的片子理一理。") {
		t.Fatalf("bytes=%d", resolved.Bytes)
	}
}

// What the client sends is two ids. Everything the message then shows — name,
// version number, digest — is the server's own reading, so a client cannot
// caption someone else's skill with a name it chose.
func TestASkillSegmentIsAnsweredEntirelyFromTheServersOwnRead(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")
	f.registerSkillTools()

	resolved, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "text", Text: "按这个来。"},
		{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Skill == nil || resolved.Skill.SegmentOrdinal != 1 {
		t.Fatalf("the skill was not fixed at the position it was named: %+v", resolved.Skill)
	}
	block := resolved.Body.Blocks[1]
	if block.Type != "skill_ref" || block.Skill == nil {
		t.Fatalf("block %+v", block)
	}
	if block.Skill.DisplayName != "参考整理与创作方向" || block.Skill.Digest != published.Digest ||
		block.Skill.VersionNumber != published.VersionNumber || block.Skill.VersionID != published.ID {
		t.Fatalf("the frozen reading does not match the published version: %+v", block.Skill)
	}
	// The instructions stay on the server. A discovery projection is not a way
	// to read a skill's text without starting a run.
	if strings.Contains(block.Text, "先检索") || block.Text != "" {
		t.Fatalf("a display block carried the instructions: %q", block.Text)
	}
	// The registered fact names the publishing account, not the reader, so
	// resolving it again never needs another account's scope.
	refs := resolved.SkillRefs()
	if len(refs) != 1 || refs[0].OwnerAccountID != platformPublisher || refs[0].SegmentOrdinal != 1 {
		t.Fatalf("skill refs %+v", refs)
	}
}

// A version id that belongs to another skill is refused as absence, not
// resolved against whichever skill was named. The pairing is the question.
func TestAVersionFromAnotherSkillIsNotResolved(t *testing.T) {
	f := setup(t)
	direction := f.publishSkill("reference-direction", "参考整理与创作方向")
	retouch := f.publishSkill("retouch-notes", "修图要点")

	_, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "skill_ref", SkillID: direction.SkillID, SkillVersionID: retouch.ID},
	})
	// The skill store's own vocabulary passes through rather than being
	// flattened: the edge already maps it, and collapsing it here would lose
	// the difference between "not there" and "its bytes are unreadable".
	if !errors.Is(err, creativeskill.ErrNotFound) {
		t.Fatalf("a crossed pairing must be absence, got %v", err)
	}
}

// A withdrawn version still renders in the history that froze it, but it
// cannot start new work. The refusal is its own answer rather than a
// validation error: the request was well formed and the photographer can act
// on it by choosing another skill.
func TestAWithdrawnVersionCannotBeSubmittedAgain(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")
	// Registered first, so the withdrawal is the only thing left to refuse it.
	f.registerSkillTools()
	if err := f.skills.DisableVersion(t.Context(), f.platform, published.ID, "首版描述有误"); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID},
	})
	if !errors.Is(err, ErrUnsupportedSegment) {
		t.Fatalf("a withdrawn version must be refused as unsupported, got %v", err)
	}
}

// Another account's private skill is not reachable by naming its ids. The
// refusal is absence, so guessing a pair learns nothing about what exists.
func TestAnotherAccountsSkillIsAbsentFromASubmission(t *testing.T) {
	f := setup(t)
	private := f.publishOwnSkill("private-line", "内部草稿")
	_, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "skill_ref", SkillID: private.SkillID, SkillVersionID: private.ID},
	})
	if !errors.Is(err, creativeskill.ErrNotFound) {
		t.Fatalf("another account's skill must be absent, got %v", err)
	}
}

// This deployment has registered no tools yet, so every skill that declares one
// is refused at submission — the same fact the catalog reports as
// available: false. The refusal is what stops a photographer from starting work
// the deployment cannot finish, and it disappears on its own once FND-08
// registers the tools rather than needing this code to change.
func TestASkillWhoseToolsAreNotRegisteredCannotBeSubmitted(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")

	_, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID},
	})
	if !errors.Is(err, ErrUnsupportedSegment) {
		t.Fatalf("a skill this deployment cannot dispatch must be refused, got %v", err)
	}
	// The same skill resolves once the tool it declares is registered, so the
	// refusal is about this deployment's capability and not about the skill.
	f.registerSkillTools()
	if _, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID},
	}); err != nil {
		t.Fatalf("registering the declared tool did not make the skill submittable: %v", err)
	}
}

// A content segment carries an id and nothing else, so the resolver has to
// decide reading rights itself. Another account's revision is absence, not a
// refusal that would confirm it exists.
func TestAContentSegmentIsReCheckedAgainstTheReadersOwnScope(t *testing.T) {
	f := setup(t)
	mine := f.revision(f.alice, "我的参考文字")
	hers := f.revision(f.bob, "她的参考文字")

	resolved, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "text", Text: "看这个："},
		{Type: "content_ref", ContentRevisionID: mine},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The body locates the revision; it never copies the words in. A message is
	// not a second home for content that already has a revision.
	if len(resolved.Body.Blocks) != 2 || resolved.Body.Blocks[1].Type != "content_ref" ||
		resolved.Body.Blocks[1].RefID != mine || resolved.Body.Blocks[1].Text != "" {
		t.Fatalf("body %+v", resolved.Body)
	}
	// One retention root, with the role that says how the message used it.
	if len(resolved.ContentRefs) != 1 || resolved.ContentRefs[0].RevisionID != mine ||
		resolved.ContentRefs[0].Role != "input" {
		t.Fatalf("content refs %+v", resolved.ContentRefs)
	}

	if _, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "content_ref", ContentRevisionID: hers},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account's revision must be absent, got %v", err)
	}
}

// Naming one revision twice is one root, not two. The body still shows both
// segments — the photographer wrote them — but the retention rows are keyed by
// revision and a duplicate would be a unique violation rather than an answer.
func TestTheSameRevisionNamedTwiceIsKeptOnce(t *testing.T) {
	f := setup(t)
	mine := f.revision(f.alice, "我的参考文字")

	resolved, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "content_ref", ContentRevisionID: mine},
		{Type: "text", Text: "还有"},
		{Type: "content_ref", ContentRevisionID: mine},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Body.Blocks) != 3 {
		t.Fatalf("the display projection dropped a segment: %+v", resolved.Body)
	}
	if len(resolved.ContentRefs) != 1 {
		t.Fatalf("one revision produced %d roots", len(resolved.ContentRefs))
	}
}

// A resolved submission becomes a message, and the skill it fixed is registered
// as a row rather than left inside the body's JSON. That row is what a later
// reader — and FND-10's collection — can query without parsing a document.
func TestAMessageRegistersTheSkillItShowed(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")
	f.registerSkillTools()
	c := f.conversation(f.alice, f.canvasID)

	resolved, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "text", Text: "按这个来。"},
		{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored Message
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		stored, err = f.service.appendMessageInTx(t.Context(), tx, c.ID, newMessage{
			Role: "user", Status: "complete", Body: resolved.Body,
			ContentRefs: resolved.ContentRefs, SkillRefs: resolved.SkillRefs(),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != 2 {
		t.Fatalf("a body with a skill block was written as v%d", stored.SchemaVersion)
	}
	if n := f.count("creative_message_skill_refs",
		"message_id=$1 AND segment_ordinal=1 AND skill_version_id=$2 AND skill_owner_account_id=$3 AND digest=$4",
		stored.ID, published.ID, platformPublisher, published.Digest); n != 1 {
		t.Fatalf("the message registered %d skill references", n)
	}
}

// RequireUsable answers "may this account display this", which is not the same
// question as "can this deployment send it to a model". It admits images, video
// and audio, so a resolver that only checks its error accepts a picture the
// model link cannot carry — which would either drop it silently or sample
// frames nobody authorised (§7.1: this phase carries readable text only).
//
// The refusal is capability, not validity: the photographer chose something
// real and can act on the difference.
func TestOnlyReadableTextContentCanBeSubmittedYet(t *testing.T) {
	f := setup(t)
	text := f.revision(f.alice, "我的参考文字")

	for _, kind := range []struct{ kind, mime string }{
		{"image", "image/jpeg"},
		{"video", "video/mp4"},
		{"audio", "audio/mpeg"},
	} {
		t.Run(kind.kind, func(t *testing.T) {
			// The premise: this revision is the account's own, ready, rooted and
			// display-granted — everything RequireUsable checks passes. The kind
			// is the only thing left to refuse it.
			media := f.mediaRevision(f.alice, kind.kind, kind.mime)
			if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				_, err := creativecontent.RequireUsable(t.Context(), tx, media, "display")
				return err
			}); err != nil {
				t.Fatalf("the fixture is not displayable, so this would pass for the wrong reason: %v", err)
			}
			_, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
				{Type: "content_ref", ContentRevisionID: media},
			})
			if !errors.Is(err, ErrUnsupportedSegment) {
				t.Fatalf("a %s reference must be refused as unsupported, got %v", kind.kind, err)
			}
		})
	}

	// The other side: text still resolves, so the check is about the kind and
	// not about content references in general.
	resolved, err := f.service.ResolveInstruction(t.Context(), f.alice, []InstructionSegment{
		{Type: "content_ref", ContentRevisionID: text},
	})
	if err != nil {
		t.Fatalf("readable text must still resolve: %v", err)
	}
	if len(resolved.ContentRefs) != 1 {
		t.Fatalf("content refs %+v", resolved.ContentRefs)
	}
}
