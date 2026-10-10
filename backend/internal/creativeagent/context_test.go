package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func contextConsent(t *testing.T, f *fixture, conversation string, ids ...string) Consent {
	t.Helper()
	receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, map[string]any{"conversation_id": conversation, "purpose": "creative_assistance", "mode": ScopeSelectedRevisions, "data_classes": []string{"text", "image_preview", "canvas_metadata"}, "selected_revision_ids": ids}))
	if err != nil {
		t.Fatal(err)
	}
	var c Consent
	if err = json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestContextToolsAndPairedHistoryReachProvider(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	revision := f.revision(f.alice, "受授权的素材正文")
	foreign := f.revision(f.bob, "绝不能外发")
	if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,title) VALUES ('ccnd_ctx','agent-a','cccv_a','core.text',0,0,'主画面')`); err != nil {
		t.Fatal(err)
	}
	consent := contextConsent(t, f, c.ID, revision)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			return scriptedResult(r, llmgateway.ToolCall{ID: "canvas", Name: readCanvasTool, Arguments: json.RawMessage(`{"offset":0,"limit":10}`)}, llmgateway.ToolCall{ID: "asset", Index: 1, Name: readAssetTool, Arguments: json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1000}`, revision))}, llmgateway.ToolCall{ID: "foreign", Index: 2, Name: readAssetTool, Arguments: json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1000}`, foreign))}, llmgateway.ToolCall{ID: "history", Index: 3, Name: readHistoryTool, Arguments: json.RawMessage(`{"before_ordinal":0,"limit":10}`)}), nil
		case 2, 3:
			calls, results := map[string]bool{}, map[string]bool{}
			for _, m := range r.Chat.Messages {
				for _, call := range m.ToolCalls {
					calls[call.ID] = true
				}
				if m.ToolCallID != "" {
					results[m.ToolCallID] = true
				}
			}
			if len(calls) != 4 || len(results) != 4 {
				t.Fatalf("lost tool history: calls=%v results=%v", calls, results)
			}
			for id := range calls {
				if !results[id] {
					t.Fatalf("orphan call %s", id)
				}
			}
		}
		return scriptedResult(r), nil
	}
	for i := 0; i < 2; i++ {
		run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "继续整理"})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
		got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
		if err != nil || got.State != RunSucceeded {
			t.Fatalf("run=%+v %v", got, err)
		}
	}
	if f.count("creative_message_content_refs", "content_revision_id=$1 AND role='result'", revision) != 1 {
		t.Fatal("tool read lost durable media root")
	}
	prompt := f.vendor.prompt()
	if !strings.Contains(prompt, "主画面") || !strings.Contains(prompt, "受授权的素材正文") || strings.Contains(prompt, "绝不能外发") {
		t.Fatalf("context authorization failed: %s", prompt)
	}
	// A read outside the grant is named as such, so the model explains what is
	// missing instead of treating it as a typo.
	if !strings.Contains(prompt, "creative_tool_not_authorized") {
		t.Fatalf("refused read not named: %s", prompt)
	}
}

type testImageReader struct{}

func (testImageReader) AgentImage(_ context.Context, _ store.AccountScope, id string, _ int64) (*llmgateway.ImageInput, error) {
	return &llmgateway.ImageInput{RevisionID: id, Digest: "sha256-" + strings.Repeat("a", 64), MIME: "image/png", ByteSize: 5, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("image")), nil }}, nil
}
func TestImageReferenceSurvivesHistoryAndNeedsRenewedConsent(t *testing.T) {
	f := setup(t)
	original, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	original.Capability.ImageInput = true
	models, err := llmgateway.NewCatalog("visual-test", original.Price.Currency, []llmgateway.ModelConfig{original})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.New(llmgateway.Config{Catalog: models, Providers: map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.vendor}, Credential: func(string) (string, bool) { return "test", true }, DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = NewService(gateway, models, f.skills)
	if err != nil {
		t.Fatal(err)
	}
	f.service.SetImageReader(testImageReader{})
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.mediaRevision(f.alice, "image", "image/png")
	textConsent := f.consent(f.alice, c.ID, id)
	if _, err := f.startRun(f.alice, c.ID, textConsent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: id}); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("image without image consent: %v", err)
	}
	consent := contextConsent(t, f, c.ID, id)
	f.vendor.script = func(ctx context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		images := 0
		for _, m := range r.Chat.Messages {
			for _, b := range m.Blocks {
				if b.Image != nil {
					images++
					if b.Image.RevisionID != id {
						t.Fatal("wrong attachment")
					}
					reader, err := b.Image.Open(ctx)
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					_ = reader.Close()
					if err != nil || string(data) != "image" {
						t.Fatalf("bytes=%q %v", data, err)
					}
				}
			}
		}
		expected := 1
		if n == 4 {
			expected = 0
		}
		if images != expected {
			t.Fatalf("turn %d: images=%d want %d", n, images, expected)
		}
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "read-image", Name: readAssetTool, Arguments: json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, id))}), nil
		}
		return scriptedResult(r), nil
	}
	for i := 0; i < 3; i++ {
		segments := []InstructionSegment{{Type: "text", Text: "描述这张图"}}
		if i == 0 {
			segments = append(segments, InstructionSegment{Type: "content_ref", ContentRevisionID: id})
		}
		if i == 2 {
			consent = f.consent(f.alice, c.ID)
		}
		run, err := f.startRun(f.alice, c.ID, consent.ID, segments...)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID)
		if got.State != RunSucceeded {
			t.Fatalf("run=%+v", got)
		}
	}
	if f.vendor.calls() != 4 {
		t.Fatalf("calls=%d", f.vendor.calls())
	}
}

// unreadableImages fails every image the way a vanished or undecodable one
// does, until it is told the image is back.
type unreadableImages struct{ readable bool }

func (u *unreadableImages) AgentImage(ctx context.Context, scope store.AccountScope, id string, limit int64) (*llmgateway.ImageInput, error) {
	if !u.readable {
		return nil, creativecontent.ErrNotFound
	}
	return testImageReader{}.AgentImage(ctx, scope, id, limit)
}

// 2026-10-10: an image the model could not receive failed its run and then
// every later run in the conversation, because it stayed in the history. The
// current message still says the input is unavailable; history drops it.
func TestUnreadableImageDoesNotPoisonTheConversation(t *testing.T) {
	f := setup(t)
	original, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	original.Capability.ImageInput = true
	models, err := llmgateway.NewCatalog("visual-test", original.Price.Currency, []llmgateway.ModelConfig{original})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.New(llmgateway.Config{Catalog: models, Providers: map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.vendor}, Credential: func(string) (string, bool) { return "test", true }, DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if f.service, err = NewService(gateway, models, f.skills); err != nil {
		t.Fatal(err)
	}
	f.service.SetImageReader(&unreadableImages{})
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	id := f.mediaRevision(f.alice, "image", "image/png")
	consent := contextConsent(t, f, c.ID, id)
	var prompt string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		prompt = ""
		for _, m := range r.Chat.Messages {
			for _, b := range m.Blocks {
				if b.Image != nil {
					t.Fatal("an unreadable image was attached")
				}
				prompt += b.Text
			}
		}
		return scriptedResult(r), nil
	}
	first, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: id}, InstructionSegment{Type: "text", Text: "优化这张图"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, first.ID); got.State != RunFailed || got.ErrorCode != "creative_input_unavailable" {
		t.Fatalf("first run %s %s", got.State, got.ErrorCode)
	}
	second, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, second.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, second.ID); got.State != RunSucceeded {
		t.Fatalf("second run %s %s", got.State, got.ErrorCode)
	}
	if !strings.Contains(prompt, "已无法读取") {
		t.Fatalf("the model was not told the image is missing: %s", prompt)
	}
}

func TestCompactionPreservesCurrentInstructionAndToolPairs(t *testing.T) {
	user := schema.UserMessage("之前的任务")
	historyTag(user, "old")
	call := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: readAssetTool, Arguments: `{}`}}}}
	historyTag(call, "old")
	result := schema.ToolMessage(strings.Repeat("正文", 3000), "call")
	historyTag(result, "old")
	current := schema.UserMessage("当前约束必须保留")
	messages := compactMessages([]*schema.Message{user, call, result, current}, 128)
	if len(messages) != 1 || messages[0] != current {
		t.Fatalf("partial turn retained: %+v", messages)
	}
	oldImage := schema.UserMessage("<<<old>>>")
	oldImage.Extra = map[string]any{imageRefsKey: "old"}
	historyTag(oldImage, "old")
	newImage := schema.UserMessage("<<<new>>>")
	newImage.Extra = map[string]any{imageRefsKey: "new"}
	limitHistoryImages([]*schema.Message{oldImage, newImage}, 1)
	if messageImageID(oldImage) != "" || !strings.Contains(oldImage.Content, "<<<old>>>") || messageImageID(newImage) != "new" {
		t.Fatal("image eviction lost reference or current image")
	}
}

func TestImageReferenceRejectedBeforeQueueForTextModel(t *testing.T) {
	f := setup(t)
	useTextOnlyModel(t, f)
	f.queue()
	f.service.SetImageReader(testImageReader{})
	c := f.conversation(f.alice, f.canvasID)
	id := f.mediaRevision(f.alice, "image", "image/png")
	consent := contextConsent(t, f, c.ID, id)
	if _, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "content_ref", ContentRevisionID: id}); !errors.Is(err, ErrModelCapability) {
		t.Fatalf("text model admitted image: %v", err)
	}
	if f.vendor.calls() != 0 || f.count("creative_agent_runs", "true") != 0 {
		t.Fatal("unsupported run created or dispatched")
	}
}

func TestImageBudgetPrioritizesCurrentReferences(t *testing.T) {
	for _, tt := range []struct {
		name                                    string
		current, count                          int
		total                                   int64
		duplicate, rewrite, wantTool, wantLimit bool
	}{
		{name: "count at binding", current: 8, count: 8, total: 100},
		{name: "count after middleware", current: 8, count: 8, total: 100, rewrite: true},
		{name: "total bytes", current: 2, count: 8, total: 10, rewrite: true},
		{name: "remaining capacity", current: 1, count: 8, total: 10, rewrite: true, wantTool: true},
		{name: "duplicate at capacity", current: 2, count: 2, total: 10, duplicate: true, rewrite: true, wantTool: true},
		{name: "explicit count overflow", current: 3, count: 2, total: 100, rewrite: true, wantLimit: true},
		{name: "explicit bytes overflow", current: 3, count: 8, total: 10, rewrite: true, wantLimit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			useModel(t, f, func(m *llmgateway.ModelConfig) { m.Capability.ImageInput = true })
			f.service.SetImageReader(testImageReader{})
			f.service.limits.ImagePreviewCount = tt.count
			f.service.limits.ImagePreviewTotalBytes = tt.total
			f.queue()
			c := f.conversation(f.alice, f.canvasID)
			ids := make([]string, tt.current+1)
			for i := range ids {
				ids[i] = f.mediaRevision(f.alice, "image", "image/png")
			}
			consent := contextConsent(t, f, c.ID, ids...)
			run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "描述图片"})
			if err != nil {
				t.Fatal(err)
			}
			held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			h := &runtimeHandler{service: f.service, scope: f.alice, held: held, revisions: ids}
			var source []*schema.Message
			for _, id := range ids[:tt.current] {
				m := schema.UserMessage(referenceLabel(id))
				m.Extra = map[string]any{imageRefsKey: id}
				source = append(source, m)
			}
			toolID := ids[tt.current]
			if tt.duplicate {
				toolID = ids[0]
			}
			args := fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, toolID)
			source = append(source, &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-image", Function: schema.FunctionCall{Name: readAssetTool, Arguments: args}}}})
			result, err := h.readAsset(t.Context(), args)
			if err != nil {
				t.Fatal(err)
			}
			source = append(source, schema.ToolMessage(result, "read-image"))
			if tt.rewrite {
				limitHistoryImages(source, tt.count)
			}
			messages := make([]llmgateway.Message, len(source))
			for i, m := range source {
				messages[i] = llmgateway.Message{Role: llmgateway.Role(m.Role), Blocks: []llmgateway.Block{{Kind: llmgateway.BlockText, Text: m.Content}}, ToolCallID: m.ToolCallID}
			}
			bound, err := h.bindMessages(t.Context(), source, messages)
			if err != nil {
				t.Fatal(err)
			}
			images := map[string]int{}
			var bytes int64
			for _, m := range bound {
				for _, b := range m.Blocks {
					if b.Image != nil {
						images[b.Image.RevisionID]++
						bytes += b.Image.ByteSize
					}
				}
			}
			if len(images) > tt.count || bytes > tt.total {
				t.Fatalf("images=%v bytes=%d", images, bytes)
			}
			if tt.wantLimit {
				if images[ids[0]] != 0 || images[ids[1]] != 1 || images[ids[2]] != 1 {
					t.Fatalf("overflow did not preserve the newest images: %v", images)
				}
				if !strings.Contains(bound[0].Blocks[0].Text, "上限") && !strings.Contains(bound[0].Blocks[0].Text, "最多允许") {
					t.Fatalf("model did not receive the limit: %+v", bound[0])
				}
				if f.count("creative_agent_blocks", "run_id=$1 AND type='notice'", run.ID) == 0 {
					t.Fatal("photographer did not receive a notice")
				}
				var code, notice string
				if err := f.db.QueryRow(`SELECT payload->>'code',payload->>'message' FROM creative_agent_blocks WHERE run_id=$1 AND source_key=$2`, run.ID, "image-limit:"+ids[0]).Scan(&code, &notice); err != nil {
					t.Fatal(err)
				}
				if tt.name == "explicit bytes overflow" {
					if code != "creative_image_total_limit" || !strings.Contains(notice, "15 字节") || !strings.Contains(notice, "10 字节") {
						t.Fatalf("total limit notice lacks actual bytes or ceiling: %s %s", code, notice)
					}
				} else if code != "creative_image_count_limit" || !strings.Contains(notice, "最多允许 2 张") {
					t.Fatalf("count limit notice lacks ceiling: %s %s", code, notice)
				}
				return
			}
			for _, id := range ids[:tt.current] {
				if images[id] != 1 {
					t.Fatalf("current image missing or duplicated: %v", images)
				}
			}
			if (images[toolID] == 1) != tt.wantTool {
				t.Fatalf("tool image selection=%v", images)
			}
			if !tt.wantTool {
				omitted := bound[len(source)-1]
				if omitted.ToolCallID != "read-image" || !strings.Contains(omitted.Blocks[0].Text, referenceLabel(toolID)) || !strings.Contains(omitted.Blocks[0].Text, "容量") {
					t.Fatalf("missing tool capacity notice: %+v", omitted)
				}
			}
		})
	}
}

// The assistant reads what is on its canvas without the photographer naming
// it first. Content elsewhere in the library still needs a reference.
func TestAssistantReadsItsOwnCanvas(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	onCanvas := f.revision(f.alice, "画布节点上的拍摄概念")
	elsewhere := f.revision(f.alice, "只在资产库里的笔记")
	if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,title,content_id,content_revision_id)
SELECT 'ccnd_concept','agent-a','cccv_a','core.text',0,0,'拍摄概念',content_id,id FROM creative_content_revisions WHERE id=$1`, onCanvas); err != nil {
		t.Fatal(err)
	}
	consent := contextConsent(t, f, c.ID)
	read := func(id string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":8192}`, id))
	}
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r,
				llmgateway.ToolCall{ID: "canvas", Name: readAssetTool, Arguments: read(onCanvas)},
				llmgateway.ToolCall{ID: "library", Index: 1, Name: readAssetTool, Arguments: read(elsewhere)}), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "读一下拍摄概念"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	prompt := f.vendor.prompt()
	if !strings.Contains(prompt, "画布节点上的拍摄概念") {
		t.Fatalf("canvas node was not readable: %s", prompt)
	}
	if strings.Contains(prompt, "只在资产库里的笔记") || !strings.Contains(prompt, "creative_tool_not_authorized") {
		t.Fatalf("library content leaked or was not refused: %s", prompt)
	}
}

// A model that cannot see images is told exactly that when it reads one on the
// canvas, not that it lacks authorisation.
func TestReadingAnImageWithoutVisionSaysSo(t *testing.T) {
	f := setup(t)
	useTextOnlyModel(t, f)
	f.service.SetImageReader(testImageReader{})
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	image := f.mediaRevision(f.alice, "image", "image/png")
	if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,title,content_id,content_revision_id)
SELECT 'ccnd_photo','agent-a','cccv_a','core.text',0,0,'木偶',content_id,id FROM creative_content_revisions WHERE id=$1`, image); err != nil {
		t.Fatal(err)
	}
	consent := f.consent(f.alice, c.ID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "photo", Name: readAssetTool, Arguments: json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1}`, image))}), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看看木偶"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if prompt := f.vendor.prompt(); !strings.Contains(prompt, "creative_tool_model_capability") {
		t.Fatalf("image refusal not named as capability: %s", prompt)
	}
}

// With a model that sees images, an image on the canvas reaches the model on
// the next turn without the photographer referencing it.
func TestCanvasImageReachesAVisionModel(t *testing.T) {
	f := setup(t)
	useModel(t, f, func(m *llmgateway.ModelConfig) { m.Capability.ImageInput = true })
	f.service.SetImageReader(testImageReader{})
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	image := f.mediaRevision(f.alice, "image", "image/png")
	if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,title,content_id,content_revision_id)
SELECT 'ccnd_photo','agent-a','cccv_a','core.text',0,0,'木偶',content_id,id FROM creative_content_revisions WHERE id=$1`, image); err != nil {
		t.Fatal(err)
	}
	consent := contextConsent(t, f, c.ID)
	sawImage := false
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			return scriptedResult(r, llmgateway.ToolCall{ID: "photo", Name: readAssetTool, Arguments: json.RawMessage(fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":1}`, image))}), nil
		}
		for _, m := range r.Chat.Messages {
			for _, b := range m.Blocks {
				sawImage = sawImage || b.Kind == llmgateway.BlockImageInput && b.Image.RevisionID == image
			}
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看看木偶"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if !sawImage {
		t.Fatalf("canvas image was not attached: %s", f.vendor.prompt())
	}
}
