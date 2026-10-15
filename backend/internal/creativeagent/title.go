package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// jobTitleKind names a conversation after its first answered run. It is a task
// of its own so the answer is never held back by a name, and a name that cannot
// be produced leaves only the default title behind.
const jobTitleKind = "creative_agent_title"

// titleCallerService meters naming apart from runs: a run's caller group is
// released and swept when the run closes, which must not reach this call.
const titleCallerService = "creative_agent_title"

const (
	titleTimeout      = 2 * time.Minute
	titleOutputLimit  = 1024
	titleMaxRunes     = 20
	titleSourceRunes  = 600
	titleSourceTotal  = 2000
	titleSourceWindow = 12
)

// errTitleSettled ends a task whose conversation already has a title other than
// the default, written by naming or by the photographer. It is not a failure,
// but the task still hands back whatever an earlier delivery's call left.
var errTitleSettled = errors.New("creative agent conversation already titled")

const titleInstruction = "你负责给一段创作对话起标题。根据对话内容，用不超过 12 个字概括创作者在讨论的主题。" +
	"只输出标题本身，不加引号、书名号、句末标点或任何解释；使用创作者所用的语言。"

type titleTask struct {
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	// ConsentID is the run's own authorisation. Naming sends the photographer's
	// words again, so it may only do so while that consent still stands.
	ConsentID string `json:"consent_id"`
	// Through freezes what is named: the messages before this ordinal when the
	// task was queued. Messages are only ever appended, so a redelivery replays
	// the paid call with the same input even after the photographer carried on.
	Through int64 `json:"through"`
}

// queueTitleInTx asks for a name once, when the conversation first has an
// answer to be named after. Later runs keep whatever name it got. A service
// without a queue skips naming rather than failing the run it is closing.
func (s *Service) queueTitleInTx(ctx context.Context, tx store.TxAccountScope, held claim, now time.Time) error {
	// A /compact run says nothing to be named after, and must not use up the
	// conversation's one naming either.
	if s.runtime == nil || held.Kind == runKindCompact {
		return nil
	}
	earlier, err := tx.Count(ctx, "creative_agent_runs",
		"conversation_id=$2 AND id<>$3 AND state IN ('succeeded','partial') AND kind='chat'", held.ConversationID, held.RunID)
	if err != nil || earlier > 0 {
		return err
	}
	var through int64
	if err := tx.QueryRow(ctx, "creative_agent_conversations", "next_message_ordinal", "id=$2", held.ConversationID).Scan(&through); err != nil {
		return err
	}
	raw, err := json.Marshal(titleTask{ConversationID: held.ConversationID, RunID: held.RunID, ConsentID: held.ConsentID, Through: through})
	if err != nil {
		return err
	}
	_, err = jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), jobs.Request{
		Kind:        jobTitleKind,
		OperationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(held.RunID+":title")).String(),
		CreatedAt:   now,
		Payload:     raw,
	})
	return err
}

// workTitle is best effort and never retried: whatever goes wrong is logged and
// the conversation keeps its default title. A redelivery after a crash replays
// the same paid call, because the binding key is the run that asked for it and
// the input is frozen in the task.
func (s *Service) workTitle(ctx context.Context, scope store.AccountScope, request jobs.Request) error {
	var task titleTask
	if err := json.Unmarshal(request.Payload, &task); err != nil || task.ConversationID == "" || task.RunID == "" ||
		task.ConsentID == "" || task.Through < 1 {
		return jobs.ErrInvalidTask
	}
	err := s.nameConversation(ctx, scope, task)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errTitleSettled) {
		s.logger.WarnContext(ctx, "creative agent conversation was not named",
			slog.String("conversation_id", task.ConversationID), slog.String("run_id", task.RunID),
			slog.String("error", err.Error()))
	}
	// Giving up is final, so what the call left behind is handed back here: the
	// run's own cleanup only reaches the run's caller service.
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return s.abandonTitleInTx(ctx, tx, task.RunID)
	}); err != nil {
		s.logger.ErrorContext(ctx, "creative agent title call was not released",
			slog.String("run_id", task.RunID), slog.String("error", err.Error()))
	}
	return nil
}

// abandonTitleInTx releases an unused hold, cancels a request that never went
// out, and gives up the retention of finished results nobody will consume.
// Requests whose outcome is unknown keep their accounting for reconciliation.
// A task that never reached the gateway has no group, and nothing to give up.
func (s *Service) abandonTitleInTx(ctx context.Context, tx store.TxAccountScope, runID string) error {
	if err := s.gateway.ReleaseUnusedGroupInTx(ctx, tx, titleCallerService, runID); err != nil {
		return err
	}
	if _, err := s.gateway.LockCallerGroupInTx(ctx, tx, titleCallerService, runID); err != nil {
		if errors.Is(err, llmgateway.ErrNotFound) {
			return nil
		}
		return err
	}
	return s.gateway.AbandonCallerGroupResultsInTx(ctx, tx, titleCallerService, runID)
}

// admitTitleInTx is the last gate, inside the transaction that records the
// dispatch intent: a consent withdrawn in a transaction that commits first
// stops the words from leaving, as it does for the run.
func admitTitleInTx(ctx context.Context, tx store.TxAccountScope, task titleTask) error {
	if err := tx.RequireCreativeCapability(ctx, "agent_start"); err != nil {
		return err
	}
	consent, err := readConsentForUpdate(ctx, tx, task.ConsentID)
	if err != nil {
		return err
	}
	return coversRun(consent, task.ConversationID, nil)
}

func (s *Service) nameConversation(ctx context.Context, scope store.AccountScope, task titleTask) error {
	// A photographer who already chose a name is not overruled, so there is
	// nothing to pay for. Renaming after this read is caught where it is written.
	var written string
	if err := scope.QueryRow(ctx, "creative_agent_conversations", "title_source",
		"id=$2", task.ConversationID).Scan(&written); err != nil {
		return err
	}
	if written != titleDefault {
		return errTitleSettled
	}
	var modelKey string
	if err := scope.QueryRow(ctx, "creative_agent_runs", "model_key",
		"id=$2 AND conversation_id=$3", task.RunID, task.ConversationID).Scan(&modelKey); err != nil {
		return err
	}
	model, err := s.models.Model(modelKey)
	if err != nil {
		return err
	}
	source, err := titleSource(ctx, scope, task.ConversationID, task.Through)
	if err != nil || source == "" {
		return err
	}
	deadline := time.Now().Add(titleTimeout)
	_, err = s.gateway.Call(ctx, llmgateway.CallSession{
		Scope:         scope,
		Limits:        func(tx store.TxAccountScope) llmgateway.LimitView { return tx.LLMLimitView() },
		CallerService: titleCallerService,
		CallerGroupID: task.RunID,
		GroupDeadline: deadline,
		ModelKey:      model.ModelKey,
		OutputLimit:   min(titleOutputLimit, model.Capability.MaxOutputTokens),
		Deadline:      deadline,
		Admit: func(tx store.TxAccountScope) error {
			return admitTitleInTx(ctx, tx, task)
		},
	}, llmgateway.CallInput{
		BindingKey: task.RunID,
		Chat: llmgateway.ChatRequest{Messages: []llmgateway.Message{
			{Role: llmgateway.RoleSystem, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: titleInstruction}}},
			{Role: llmgateway.RoleUser, Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: source}}},
		}},
		Consume: func(tx store.TxAccountScope, result llmgateway.Result) error {
			title := cleanTitle(result.Text)
			if !result.Consumable() || title == "" {
				return nil
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return err
			}
			// Only a default title is replaced: one the photographer typed
			// meanwhile wins, even over a name already paid for.
			_, err = tx.Update(ctx, "creative_agent_conversations",
				"title=$3, title_source='generated', revision=revision+1, updated_at=$4",
				"id=$2 AND title_source='default'", task.ConversationID, title, now)
			return err
		},
	})
	return err
}

// titleSource is the opening of the conversation as plain dialogue: what the
// photographer asked and what was answered, without tool traffic or quotes.
// Only messages before through count, so the same task always reads the same.
func titleSource(ctx context.Context, scope store.AccountScope, conversationID string, through int64) (string, error) {
	rows, err := scope.QueryPage(ctx, "creative_agent_messages", "role, body",
		"conversation_id=$2 AND ordinal<$3 AND role IN ('user','assistant') AND status='complete'",
		[]store.OrderBy{{Column: "ordinal"}}, titleSourceWindow, 0, conversationID, through)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var role string
		var payload []byte
		if err := rows.Scan(&role, &payload); err != nil {
			return "", err
		}
		var body Body
		if err := json.Unmarshal(payload, &body); err != nil {
			return "", err
		}
		var text strings.Builder
		for _, block := range body.Blocks {
			if block.Type == "text" {
				text.WriteString(block.Text)
			}
		}
		line := strings.TrimSpace(text.String())
		if line == "" {
			continue
		}
		speaker := "助手"
		if role == "user" {
			speaker = "创作者"
		}
		b.WriteString(speaker + "：" + runePrefix(line, titleSourceRunes) + "\n")
		if utf8.RuneCountInString(b.String()) >= titleSourceTotal {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(runePrefix(b.String(), titleSourceTotal)), nil
}

// cleanTitle keeps the first line of the answer and strips what models add
// around a title despite being told not to.
func cleanTitle(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	for _, prefix := range []string{"标题：", "标题:", "Title:", "title:"} {
		line = strings.TrimPrefix(line, prefix)
	}
	line = strings.TrimFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || strings.ContainsRune("《》「」『』“”‘’\"'`*#", r)
	})
	return runePrefix(line, titleMaxRunes)
}

func runePrefix(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
