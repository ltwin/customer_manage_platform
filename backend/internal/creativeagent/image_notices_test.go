package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type oversizedImageReader struct{}

func (oversizedImageReader) AgentImage(_ context.Context, _ store.AccountScope, _ string, limit int64) (*llmgateway.ImageInput, error) {
	return nil, &creativemedia.ImagePreviewLimitError{SizeBytes: 25 << 20, LimitBytes: limit}
}

func TestImageLimitNoticeRequiresCurrentWorker(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	held.Epoch--
	h := &runtimeHandler{service: f.service, scope: f.alice, held: held}
	source := schema.UserMessage("引用图片")
	message := llmgateway.Message{Role: llmgateway.RoleUser}
	err = h.omitLimitedImage(t.Context(), source, &message, "old-worker-image", "creative_image_size_limit", "图片超限。")
	if !errors.Is(err, ErrRunState) || f.count("creative_agent_blocks", "run_id=$1", run.ID) != 0 {
		t.Fatalf("stale worker wrote a notice: %v", err)
	}
	if source.Content != "引用图片" {
		t.Fatal("a failed notice write changed the prompt")
	}
}

// The first turn, a repeated tool read, and later text-only turns all continue.
// The visible notice is persisted even when the model never mentions the limit.
func TestOversizedImageIsANoticeAndConversationContinues(t *testing.T) {
	f := setup(t)
	useModel(t, f, func(m *llmgateway.ModelConfig) { m.Capability.ImageInput = true })
	f.service.SetImageReader(oversizedImageReader{})
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.mediaRevision(f.alice, "image", "image/png")
	consent := contextConsent(t, f, c.ID, id)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		var text strings.Builder
		for _, m := range r.Chat.Messages {
			for _, b := range m.Blocks {
				if b.Image != nil {
					t.Fatal("oversized image reached the provider")
				}
				text.WriteString(b.Text)
			}
		}
		for _, want := range []string{referenceLabel(id), "25.00 MiB", "20.00 MiB", "图片未加载"} {
			if !strings.Contains(text.String(), want) {
				t.Fatalf("model was not told %q: %s", want, text.String())
			}
		}
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "read-again", Name: readAssetTool, Arguments: json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, id))}), nil
		}
		return scriptedResult(r), nil
	}
	for turn := 0; turn < 3; turn++ {
		segments := []InstructionSegment{{Type: "text", Text: "继续聊拍摄"}}
		if turn == 0 {
			segments = append(segments, InstructionSegment{Type: "content_ref", ContentRevisionID: id})
		}
		run, err := f.startRun(f.alice, c.ID, consent.ID, segments...)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
		got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
		if err != nil || got.State != RunSucceeded || got.ErrorCode != "" {
			t.Fatalf("turn %d: run=%+v error=%v", turn, got, err)
		}
		if count := f.count("creative_agent_blocks", "run_id=$1 AND type='notice'", run.ID); count != 1 {
			t.Fatalf("turn %d: got %d notices, want one despite repeated references", turn, count)
		}
		var notice noticePayload
		var raw []byte
		if err = f.db.QueryRow(`SELECT payload FROM creative_agent_blocks WHERE run_id=$1 AND type='notice'`, run.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &notice); err != nil {
			t.Fatal(err)
		}
		if notice.Code != "creative_image_size_limit" || !strings.Contains(notice.Message, "25.00 MiB") || !strings.Contains(notice.Message, "20.00 MiB") || !strings.Contains(notice.Message, "对话可以继续") {
			t.Fatalf("notice is not actionable: %+v", notice)
		}
	}
	if f.vendor.calls() != 4 {
		t.Fatalf("conversation did not continue: %d model calls", f.vendor.calls())
	}
}
