package creativeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Only synthetic pixels leave the isolated test account. Generation is stubbed;
// the real model must discover and call the search/read/edit/generate tools.
type lookupLiveImage struct{ data []byte }

func (r lookupLiveImage) AgentImage(_ context.Context, _ store.AccountScope, id string, _ int64) (*llmgateway.ImageInput, error) {
	return &llmgateway.ImageInput{RevisionID: id, Digest: fmt.Sprintf("sha256-%x", sha256.Sum256(r.data)), MIME: "image/png", ByteSize: int64(len(r.data)), Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(r.data)), nil }}, nil
}
func TestLiveAgentAssetLookup(t *testing.T) {
	if os.Getenv("CREATIVE_AGENT_LIVE") != "1" || strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY")) == "" {
		t.Skip("explicit live acceptance not enabled")
	}
	for _, source := range []string{"library", "conversation"} {
		t.Run(source, func(t *testing.T) {
			f := setup(t)
			defaults, err := llmgateway.DefaultCatalog(nil)
			if err != nil {
				t.Fatal(err)
			}
			model, err := defaults.Model("gpt-6.1-sol")
			if err != nil {
				t.Fatal(err)
			}
			model.Capability.MaxOutputTokens = 2048
			catalog, err := llmgateway.NewCatalog("asset-lookup-live", model.Price.Currency, []llmgateway.ModelConfig{model})
			if err != nil {
				t.Fatal(err)
			}
			gateway, err := llmgateway.Build(llmgateway.Options{Catalog: catalog, MonthlyLimitMicros: 2_000_000, MonthlyTokenLimit: 200_000, Timeout: 2 * time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			f.service, err = NewService(gateway, catalog, f.skills)
			if err != nil {
				t.Fatal(err)
			}
			img := image.NewRGBA(image.Rect(0, 0, 64, 64))
			for y := 0; y < 64; y++ {
				for x := 0; x < 64; x++ {
					img.Set(x, y, color.RGBA{R: uint8(x * 4), G: 120, B: uint8(y * 4), A: 255})
				}
			}
			var data bytes.Buffer
			if err := png.Encode(&data, img); err != nil {
				t.Fatal(err)
			}
			f.service.limits.InputTextBytes = 64 << 10
			f.service.limits.RunDurationSeconds = 240
			f.service.SetImageReader(lookupLiveImage{data: data.Bytes()})
			generator := &stubGenerator{}
			f.service.SetGenerator(generator)
			f.queue()
			id := f.mediaRevision(f.alice, "image", "image/png")
			var asset string
			if err := f.db.QueryRow(`SELECT id FROM creative_assets WHERE account_id='agent-a' AND content_revision_id=$1`, id).Scan(&asset); err != nil {
				t.Fatal(err)
			}
			title := "琥珀蓝渐变参考"
			description := "用于创意配色的合成色块图片"
			if _, err := creativelibrary.Metadata(t.Context(), f.alice, command(t, creativelibrary.MetadataInput{ID: asset, ExpectedRevision: 1, Title: &title, Description: &description})); err != nil {
				t.Fatal(err)
			}
			c := f.conversation(f.alice, f.canvasID)
			prompt := "请直接完成：去资产库查找「琥珀蓝渐变参考」，读取图片看配色，把找到的素材放上画布，再新建一个图片节点，把参考节点连到新节点，用 GPT Image 2 生成这种配色的极简海报。必须先查看参考图，不要自行编造素材。"
			if source == "conversation" {
				appendLookupMessage(t, f, c.ID, []ContentRef{{RevisionID: id, Role: "input"}}, "这是我上传的琥珀蓝渐变参考图")
				for range 25 {
					appendLookupMessage(t, f, c.ID, nil, "继续讨论海报构图")
				}
				if _, err := f.db.Exec(`DELETE FROM creative_assets WHERE account_id='agent-a' AND id=$1`, asset); err != nil {
					t.Fatal(err)
				}
				prompt = "请直接完成：找回我在本对话很早以前上传的琥珀蓝渐变参考图，读取图片看配色，把它放上画布，再新建一个图片节点并把参考节点连到新节点，用 GPT Image 2 生成这种配色的极简海报。参考图没有存入资产库，请从对话内容找回。"
			}
			consent := libraryConsent(t, f, c.ID)
			receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{"conversation_id": c.ID, "model_key": "gpt-6.1-sol", "egress_consent_id": consent.ID, "instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{{Type: "text", Text: prompt}}}}))
			if err != nil {
				t.Fatal(err)
			}
			var run Run
			if err := json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
				t.Fatal(err)
			}
			workErr := f.work(f.alice, run.ID)
			rows, err := f.db.Query(`SELECT tool_key,state,input::text,output::text FROM creative_agent_steps WHERE account_id='agent-a' AND run_id=$1 AND kind='tool' ORDER BY ordinal`, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			tools := map[string]bool{}
			for rows.Next() {
				var key, state, in, out string
				if err := rows.Scan(&key, &state, &in, &out); err != nil {
					_ = rows.Close() // Rows.Err or the scan error remains authoritative.
					t.Fatal(err)
				}
				tools[key] = state == "succeeded"
				t.Logf("tool=%s state=%s input=%s output=%s", key, state, in, out)
			}
			err = rows.Err()
			_ = rows.Close() // Rows.Err or the scan error remains authoritative.
			if err != nil {
				t.Fatal(err)
			}
			if workErr != nil {
				t.Fatal(workErr)
			}
			got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil || got.State != RunSucceeded {
				t.Fatalf("run=%+v error=%v", got, err)
			}
			discover := searchAssetsTool
			if source == "conversation" {
				discover = conversationAssetsTool
			}
			for _, key := range []string{discover, readAssetTool, editCanvasTool, setNodePromptTool, generateNodeTool} {
				if !tools[key] {
					t.Errorf("real model did not complete %s", key)
				}
			}
			if len(generator.calls) != 1 || f.count("creative_edges", "canvas_id=$1", f.canvasID) != 1 {
				t.Fatal("reference generation flow incomplete")
			}
		})
	}
}
