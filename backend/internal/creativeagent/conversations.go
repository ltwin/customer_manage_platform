package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Ordinal is a conversation-scoped message position. Like a revision it crosses
// the API as a decimal string, because a browser's float64 silently rounds the
// large integers this counter will eventually reach.
type Ordinal int64

func (o Ordinal) MarshalJSON() ([]byte, error) { return creativeops.Revision(o).MarshalJSON() }
func (o *Ordinal) UnmarshalJSON(data []byte) error {
	var r creativeops.Revision
	if err := r.UnmarshalJSON(data); err != nil {
		return err
	}
	*o = Ordinal(r)
	return nil
}

type Conversation struct {
	ID        string               `json:"id"`
	ProjectID string               `json:"project_id"`
	CanvasID  string               `json:"canvas_id"`
	Title     string               `json:"title"`
	Revision  creativeops.Revision `json:"revision"`
	UpdatedAt time.Time            `json:"updated_at"`
}

type ConversationPage struct {
	Items      []Conversation `json:"items"`
	NextCursor string         `json:"next_cursor"`
}

// Block is one renderable unit of a message. Only text carries words; the other
// kinds locate something instead of copying it, so a message never becomes a
// second home for content that already has a revision.
type Block struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// RefID locates a content revision or tool result; it grants nothing on its
	// own.
	RefID string `json:"ref_id,omitempty"`
	// Code is a stable machine reason on a notice, e.g. a refusal.
	Code string `json:"code,omitempty"`
	// Skill is the server's own reading of a skill_ref segment. It is a nested
	// value rather than five more flat fields, because it is one fact — which
	// frozen version this message showed — and a half-filled one means nothing.
	Skill *BlockSkill `json:"skill,omitempty"`
}

// BlockSkill is what a message froze about a skill: the identity to resolve it
// again and the name and number it displayed at the time. A later rename must
// not rewrite history, so the display fields are a snapshot, not a lookup.
type BlockSkill struct {
	SkillID       string `json:"skill_id"`
	VersionID     string `json:"skill_version_id"`
	VersionNumber int    `json:"version_number"`
	Digest        string `json:"digest"`
	DisplayName   string `json:"display_name"`
}

type Body struct {
	Blocks []Block `json:"blocks"`
}

type Message struct {
	ID            string               `json:"id"`
	Ordinal       Ordinal              `json:"ordinal"`
	RunID         string               `json:"run_id,omitempty"`
	Role          string               `json:"role"`
	Status        string               `json:"status"`
	SchemaVersion int                  `json:"schema_version"`
	Body          Body                 `json:"body"`
	Revision      creativeops.Revision `json:"revision"`
	CreatedAt     time.Time            `json:"created_at"`
}

type MessagePage struct {
	Items []Message `json:"items"`
	// PrevCursor pages further back in time; an empty value means the
	// conversation starts here.
	PrevCursor           string               `json:"prev_cursor"`
	ConversationRevision creativeops.Revision `json:"conversation_revision"`
}

// messageSchemaVersion is the persisted body format. v2 names skill and content
// references explicitly instead of leaving them as an untyped "reference"
// locator, so a reader can tell a frozen skill from a content revision without
// guessing at the id's prefix.
//
// Existing v1 rows keep their own number and read unchanged — the read path
// does not revalidate a stored body, and an unknown historical reference is
// never re-interpreted as a skill.
const messageSchemaVersion = 2

// newMessage is trusted server-side input. Clients never post a message
// directly: one arrives with the run that triggered it, or from a step result.
type newMessage struct {
	Role   string
	Status string
	Body   Body
	RunID  string
	// SourceStepID makes a step's projection idempotent. Replaying a consumed
	// result returns the original message instead of appending a duplicate.
	SourceStepID string
	// ContentRefs are the revisions this message will keep readable after the
	// run payload expires. Identifiers buried in the body are not roots.
	ContentRefs []ContentRef
	// SkillRefs registers which frozen version this message showed. The body
	// renders it; this is the fact a later reader — and FND-10's collection —
	// can query without parsing JSON.
	SkillRefs []SkillRef
}

type ContentRef struct {
	RevisionID string
	Role       string
}

// SkillRef is one message's use of one frozen version, keyed by the segment it
// was named at. OwnerAccountID is recorded as a value rather than looked up
// later: a platform skill belongs to the trusted publisher, and resolving it
// again must never need another account's scope.
type SkillRef struct {
	SegmentOrdinal int
	SkillID        string
	VersionID      string
	OwnerAccountID string
	Digest         string
}

// SkillRefs projects a resolved submission into the rows a message registers.
// The resolver already proved every field; this only changes its shape.
func (r ResolvedInstruction) SkillRefs() []SkillRef {
	if r.Skill == nil {
		return nil
	}
	return []SkillRef{{
		SegmentOrdinal: r.Skill.SegmentOrdinal,
		SkillID:        r.Skill.Snapshot.SkillID,
		VersionID:      r.Skill.Snapshot.ID,
		OwnerAccountID: r.Skill.Snapshot.OwnerAccountID,
		Digest:         r.Skill.Snapshot.Digest,
	}}
}

func validBody(b Body, limits Limits) error {
	if len(b.Blocks) == 0 || len(b.Blocks) > maxBlocks {
		return creativeops.ErrValidation
	}
	for _, block := range b.Blocks {
		switch block.Type {
		case "text":
			if block.Text == "" || block.RefID != "" || block.Code != "" || block.Skill != nil {
				return creativeops.ErrValidation
			}
		case "content_ref", "tool_result":
			if block.RefID == "" || block.Code != "" || block.Skill != nil {
				return creativeops.ErrValidation
			}
		case "skill_ref":
			// A skill block is the one kind whose payload the server wrote, so
			// every part of that reading has to be present. A half-filled one
			// would render a version nobody can resolve again.
			if block.Skill == nil || block.Skill.SkillID == "" || block.Skill.VersionID == "" ||
				block.Skill.VersionNumber < 1 || block.Skill.Digest == "" || block.Skill.DisplayName == "" ||
				block.Text != "" || block.RefID != "" || block.Code != "" {
				return creativeops.ErrValidation
			}
		case "notice":
			if block.Code == "" || block.RefID != "" || block.Skill != nil {
				return creativeops.ErrValidation
			}
		default:
			return creativeops.ErrValidation
		}
		if !utf8.ValidString(block.Text) || !utf8.ValidString(block.RefID) || !utf8.ValidString(block.Code) {
			return creativeops.ErrValidation
		}
		if block.Skill != nil && (!utf8.ValidString(block.Skill.DisplayName) || !utf8.ValidString(block.Skill.SkillID) ||
			!utf8.ValidString(block.Skill.VersionID) || !utf8.ValidString(block.Skill.Digest)) {
			return creativeops.ErrValidation
		}
	}
	if bodyBytes(b) > limits.MessageBodyBytes {
		return ErrLimit
	}
	return nil
}

// bodyBytes is what a body costs against the budget: every stored string, not
// only the words. A skill block carries no prose but still occupies a row and a
// payload, and charging it nothing would let 200 of them cost zero.
func bodyBytes(b Body) int {
	total := 0
	for _, block := range b.Blocks {
		total += len(block.Text) + len(block.RefID) + len(block.Code)
		if block.Skill != nil {
			total += len(block.Skill.SkillID) + len(block.Skill.VersionID) +
				len(block.Skill.Digest) + len(block.Skill.DisplayName)
		}
	}
	return total
}

// appendMessageInTx takes the next ordinal under the conversation row lock, so
// two runs writing at once can never land on the same position. Callers hold
// the locks that precede conversation in the global order.
func (s *Service) appendMessageInTx(ctx context.Context, tx store.TxAccountScope, conversationID string, m newMessage) (Message, error) {
	if err := validBody(m.Body, s.limits); err != nil {
		return Message{}, err
	}
	switch m.Role {
	case "user", "assistant", "tool", "system":
	default:
		return Message{}, creativeops.ErrValidation
	}
	switch m.Status {
	case "streaming", "complete", "interrupted":
	default:
		return Message{}, creativeops.ErrValidation
	}
	for i, ref := range m.ContentRefs {
		if ref.RevisionID == "" || (ref.Role != "input" && ref.Role != "attachment" && ref.Role != "result") {
			return Message{}, creativeops.ErrValidation
		}
		for _, earlier := range m.ContentRefs[:i] {
			if earlier == ref {
				return Message{}, creativeops.ErrValidation
			}
		}
	}
	for i, ref := range m.SkillRefs {
		if ref.SegmentOrdinal < 0 || ref.SkillID == "" || ref.VersionID == "" ||
			ref.OwnerAccountID == "" || ref.Digest == "" {
			return Message{}, creativeops.ErrValidation
		}
		for _, earlier := range m.SkillRefs[:i] {
			// The ordinal is the primary key's last column, so a repeat would
			// surface as a raw unique violation instead of a typed refusal.
			if earlier.SegmentOrdinal == ref.SegmentOrdinal {
				return Message{}, creativeops.ErrValidation
			}
		}
	}
	var ordinal int64
	var revision int64
	err := tx.QueryRowForUpdate(ctx, "creative_agent_conversations", "next_message_ordinal, revision", "id=$2", conversationID).Scan(&ordinal, &revision)
	if errors.Is(err, store.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	// The replay probe comes after the conversation row lock. A step belongs to
	// a run and a run to one conversation, so two consumers of the same step
	// contend for this very lock; probing before it would let both miss and the
	// loser would hit the unique index as a raw error instead of replaying.
	//
	// Replay identity is (step, role) only: the body is not compared. A second
	// consumption of one step carrying different words returns the first
	// message rather than reporting a conflict.
	if m.SourceStepID != "" {
		existing, err := s.messageForStepInTx(ctx, tx, conversationID, m.SourceStepID, m.Role)
		if err == nil {
			return existing, nil // Replay: this step already produced its message.
		}
		if !errors.Is(err, ErrNotFound) {
			return Message{}, err
		}
	}
	payload, err := json.Marshal(m.Body)
	if err != nil {
		return Message{}, err
	}
	id, err := creativeops.NewResourceID("ccms")
	if err != nil {
		return Message{}, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return Message{}, err
	}
	var runID, stepID *string
	if m.RunID != "" {
		runID = &m.RunID
	}
	if m.SourceStepID != "" {
		stepID = &m.SourceStepID
	}
	if err := tx.Insert(ctx, "creative_agent_messages",
		[]string{"id", "conversation_id", "ordinal", "run_id", "role", "status", "schema_version", "body", "source_step_id", "created_at", "updated_at"},
		id, conversationID, ordinal, runID, m.Role, m.Status, messageSchemaVersion, payload, stepID, now, now); err != nil {
		return Message{}, err
	}
	for _, ref := range m.ContentRefs {
		if err := tx.Insert(ctx, "creative_message_content_refs",
			[]string{"message_id", "content_revision_id", "role"}, id, ref.RevisionID, ref.Role); err != nil {
			return Message{}, err
		}
	}
	for _, ref := range m.SkillRefs {
		if err := tx.Insert(ctx, "creative_message_skill_refs",
			[]string{"message_id", "segment_ordinal", "skill_id", "skill_version_id", "skill_owner_account_id", "digest"},
			id, ref.SegmentOrdinal, ref.SkillID, ref.VersionID, ref.OwnerAccountID, ref.Digest); err != nil {
			return Message{}, err
		}
	}
	if _, err := tx.Update(ctx, "creative_agent_conversations",
		"next_message_ordinal=next_message_ordinal+1, revision=revision+1, updated_at=$3", "id=$2", conversationID, now); err != nil {
		return Message{}, err
	}
	return Message{ID: id, Ordinal: Ordinal(ordinal), RunID: m.RunID, Role: m.Role, Status: m.Status,
		SchemaVersion: messageSchemaVersion, Body: m.Body, Revision: 1, CreatedAt: now}, nil
}

func (s *Service) messageForStepInTx(ctx context.Context, tx store.TxAccountScope, conversationID, stepID, role string) (Message, error) {
	// Scoped to the conversation as well as the step: a caller naming the wrong
	// conversation must get nothing back, not another conversation's message.
	row := tx.QueryRow(ctx, "creative_agent_messages",
		"id, ordinal, run_id, role, status, schema_version, body, revision, created_at",
		"conversation_id=$2 AND source_step_id=$3 AND role=$4", conversationID, stepID, role)
	m, err := scanMessage(row)
	if errors.Is(err, store.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return m, err
}

type messageRow interface {
	Scan(dest ...any) error
}

func scanMessage(row messageRow) (Message, error) {
	var m Message
	var ordinal, revision int64
	var runID *string
	var payload []byte
	if err := row.Scan(&m.ID, &ordinal, &runID, &m.Role, &m.Status, &m.SchemaVersion, &payload, &revision, &m.CreatedAt); err != nil {
		return Message{}, err
	}
	if err := json.Unmarshal(payload, &m.Body); err != nil {
		return Message{}, err
	}
	m.Ordinal, m.Revision = Ordinal(ordinal), creativeops.Revision(revision)
	if runID != nil {
		m.RunID = *runID
	}
	return m, nil
}

type createConversation struct {
	CanvasID string `json:"canvas_id"`
	Title    string `json:"title"`
}

// CreateConversation binds a conversation to one canvas and records the project
// that canvas actually belongs to, rather than trusting a client-supplied pair.
func (s *Service) CreateConversation(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var input createConversation
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.create_conversation", Capability: "agent_start",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			if input.CanvasID == "" {
				return creativeops.ErrValidation
			}
			title := utf8.RuneCountInString(input.Title)
			if title < 1 || title > 120 {
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			var projectID string
			err := tx.QueryRow(ctx, "creative_canvases", "project_id", "id=$2", input.CanvasID).Scan(&projectID)
			if errors.Is(err, store.ErrNoRows) {
				return creativeops.Outcome{}, ErrNotFound
			}
			if err != nil {
				return creativeops.Outcome{}, err
			}
			id, err := creativeops.NewResourceID("ccco")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err := tx.Insert(ctx, "creative_agent_conversations",
				[]string{"id", "project_id", "canvas_id", "title", "created_at", "updated_at"},
				id, projectID, input.CanvasID, input.Title, now, now); err != nil {
				return creativeops.Outcome{}, err
			}
			c := Conversation{ID: id, ProjectID: projectID, CanvasID: input.CanvasID, Title: input.Title, Revision: 1, UpdatedAt: now}
			response, err := json.Marshal(c)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			revision := c.Revision
			return creativeops.Outcome{HTTPStatus: 201, Response: response, ResultKind: "agent_conversation", ResultID: &id, ResultRevision: &revision}, nil
		},
	}, command)
}

func (s *Service) ListConversations(ctx context.Context, scope store.AccountScope, canvasID string, limit int, cursor string) (ConversationPage, error) {
	if canvasID == "" || limit < 1 || limit > 100 {
		return ConversationPage{}, creativeops.ErrValidation
	}
	query := "agent_conversations:" + canvasID
	key, err := creativeops.DecodePageCursor(cursor, scope.AccountID(), query)
	if err != nil {
		return ConversationPage{}, err
	}
	page := ConversationPage{Items: []Conversation{}}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		cond := "canvas_id=$2"
		args := []any{canvasID}
		if cursor != "" {
			cond += " AND (updated_at,id)<($3,$4)"
			args = append(args, key.Time, key.ID)
		}
		rows, err := tx.QueryPage(ctx, "creative_agent_conversations", "id,project_id,canvas_id,title,revision,updated_at", cond,
			[]store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, limit+1, 0, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Conversation
			var revision int64
			if err := rows.Scan(&c.ID, &c.ProjectID, &c.CanvasID, &c.Title, &revision, &c.UpdatedAt); err != nil {
				return err
			}
			c.Revision = creativeops.Revision(revision)
			page.Items = append(page.Items, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			last := page.Items[limit-1]
			page.NextCursor, err = creativeops.EncodePageCursor(scope.AccountID(), query, last.UpdatedAt, last.ID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ConversationPage{}, err
	}
	return page, nil
}

// ListMessages pages backwards from the newest message, returning each page in
// reading order. beforeOrdinal is exclusive and empty means "from the end".
func (s *Service) ListMessages(ctx context.Context, scope store.AccountScope, conversationID, beforeOrdinal string, limit int) (MessagePage, error) {
	if conversationID == "" || limit < 1 || limit > 100 {
		return MessagePage{}, creativeops.ErrValidation
	}
	before := int64(0)
	if beforeOrdinal != "" {
		parsed, err := strconv.ParseInt(beforeOrdinal, 10, 64)
		if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != beforeOrdinal {
			return MessagePage{}, creativeops.ErrValidation
		}
		before = parsed
	}
	page := MessagePage{Items: []Message{}}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var revision int64
		err := tx.QueryRow(ctx, "creative_agent_conversations", "revision", "id=$2", conversationID).Scan(&revision)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		page.ConversationRevision = creativeops.Revision(revision)
		cond := "conversation_id=$2"
		args := []any{conversationID}
		if before > 0 {
			cond += " AND ordinal<$3"
			args = append(args, before)
		}
		rows, err := tx.QueryPage(ctx, "creative_agent_messages",
			"id, ordinal, run_id, role, status, schema_version, body, revision, created_at", cond,
			[]store.OrderBy{{Column: "ordinal", Desc: true}}, limit+1, 0, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, m)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.PrevCursor = strconv.FormatInt(int64(page.Items[limit-1].Ordinal), 10)
		}
		for i, j := 0, len(page.Items)-1; i < j; i, j = i+1, j-1 {
			page.Items[i], page.Items[j] = page.Items[j], page.Items[i]
		}
		return nil
	})
	if err != nil {
		return MessagePage{}, err
	}
	return page, nil
}
