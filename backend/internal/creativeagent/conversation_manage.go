package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Renaming and archiving are the photographer tidying their own list. Neither
// touches a message, a run or anything a run retains, and neither moves the
// conversation in the list: updated_at stays the time of the last exchange.
//
// Neither asks for an expected revision either. Every message bumps the
// conversation's revision, so a panel would lose the race to its own answer;
// both commands set a value outright and replay by operation.

type conversationTarget struct {
	ConversationID string `json:"conversation_id"`
}

type renameConversation struct {
	ConversationID string `json:"conversation_id"`
	Title          string `json:"title"`
}

// RenameConversation records the photographer's own title. From then on
// automatic naming leaves the conversation alone.
func (s *Service) RenameConversation(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var input renameConversation
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "agent.rename_conversation", Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			input.Title = strings.TrimSpace(input.Title)
			title := utf8.RuneCountInString(input.Title)
			if input.ConversationID == "" || title < 1 || title > 120 {
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			c, err := lockConversationInTx(ctx, tx, input.ConversationID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if c.Archived {
				return creativeops.Outcome{}, ErrConversationArchived
			}
			if _, err := tx.Update(ctx, "creative_agent_conversations",
				"title=$3, title_source='manual', revision=revision+1", "id=$2", c.ID, input.Title); err != nil {
				return creativeops.Outcome{}, err
			}
			c.Title, c.TitleSource, c.Revision = input.Title, titleManual, c.Revision+1
			return conversationOutcome(c)
		},
	}, command)
}

// ArchiveConversation hides a finished conversation from the list and stops new
// runs in it until it is restored.
func (s *Service) ArchiveConversation(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return s.setConversationArchived(ctx, scope, command, true)
}

func (s *Service) RestoreConversation(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return s.setConversationArchived(ctx, scope, command, false)
}

func (s *Service) setConversationArchived(ctx context.Context, scope store.AccountScope, command creativeops.Command, archive bool) (creativeops.Receipt, error) {
	key := "agent.restore_conversation"
	if archive {
		key = "agent.archive_conversation"
	}
	var input conversationTarget
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: key, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &input); err != nil {
				return err
			}
			if input.ConversationID == "" {
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			c, err := lockConversationInTx(ctx, tx, input.ConversationID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if c.Archived == archive {
				return conversationOutcome(c)
			}
			set := "archived_at=NULL, revision=revision+1"
			if archive {
				// A run still going would vanish from the panel mid-answer, and a
				// waiting one would wait on a conversation nobody can see. Runs
				// are created under this same row lock, so none can start now.
				open, err := tx.Exists(ctx, "creative_agent_runs", "conversation_id=$2 AND finished_at IS NULL", c.ID)
				if err != nil {
					return creativeops.Outcome{}, err
				}
				if open {
					return creativeops.Outcome{}, ErrRunState
				}
				set = "archived_at=clock_timestamp(), revision=revision+1"
			}
			if _, err := tx.Update(ctx, "creative_agent_conversations", set, "id=$2", c.ID); err != nil {
				return creativeops.Outcome{}, err
			}
			c.Archived, c.Revision = archive, c.Revision+1
			return conversationOutcome(c)
		},
	}, command)
}

func lockConversationInTx(ctx context.Context, tx store.TxAccountScope, id string) (Conversation, error) {
	c, err := scanConversation(tx.QueryRowForUpdate(ctx, "creative_agent_conversations", conversationColumns, "id=$2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	return c, err
}

// lockOpenConversationInTx is where a new run takes its conversation: under the
// row lock archiving also takes, and only while the conversation is open.
func lockOpenConversationInTx(ctx context.Context, tx store.TxAccountScope, id string) (Conversation, error) {
	c, err := lockConversationInTx(ctx, tx, id)
	if err == nil && c.Archived {
		return Conversation{}, ErrConversationArchived
	}
	return c, err
}

func conversationOutcome(c Conversation) (creativeops.Outcome, error) {
	response, err := json.Marshal(c)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	return creativeops.Outcome{HTTPStatus: 200, Response: response, ResultKind: "agent_conversation", ResultID: &c.ID, ResultRevision: &c.Revision}, nil
}
