package creativeagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func liveLibraryFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	if os.Getenv("CREATIVE_AGENT_LIVE") != "1" || strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY")) == "" {
		t.Skip("explicit live acceptance not enabled")
	}
	f := setup(t)
	defaults, err := llmgateway.DefaultCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	model, err := defaults.Model("gpt-6.1-sol")
	if err != nil {
		t.Fatal(err)
	}
	model.Capability.MaxOutputTokens = 8192
	catalog, err := llmgateway.NewCatalog("library-organize-live", model.Price.Currency, []llmgateway.ModelConfig{model})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.Build(llmgateway.Options{Catalog: catalog, MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 2_000_000, Timeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = NewService(gateway, catalog, f.skills)
	if err != nil {
		t.Fatal(err)
	}
	return f, model.Price.Currency
}

func TestLiveAgentLibraryOrganizeTwentyImages(t *testing.T) {
	f, currency := liveLibraryFixture(t)
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: 120, B: uint8(y * 4), A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	f.service.SetImageReader(lookupLiveImage{data: data.Bytes()})
	f.queue()
	revisions := map[string]string{}
	for i := range 20 {
		id := f.mediaRevision(f.alice, "image", "image/png")
		var asset string
		if err := f.db.QueryRow(`SELECT id FROM creative_assets WHERE account_id='agent-a' AND content_revision_id=$1`, id).Scan(&asset); err != nil {
			t.Fatal(err)
		}
		revisions[asset] = id
		title := fmt.Sprintf("合成配色参考%02d", i)
		empty := ""
		if _, err := creativelibrary.Metadata(t.Context(), f.alice, command(t, creativelibrary.MetadataInput{ID: asset, ExpectedRevision: 1, Title: &title, Description: &empty})); err != nil {
			t.Fatal(err)
		}
	}
	c := f.conversation(f.alice, f.canvasID)
	consent := libraryConsent(t, f, c.ID)
	receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{"conversation_id": c.ID, "model_key": "gpt-6.1-sol", "egress_consent_id": consent.ID, "instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{{Type: "text", Text: "请整理资产库中全部 20 张合成配色参考图片：逐张查看图片，按画面补写空描述，并创建和添加合适的标签。每张都要查看后再写，不改任何已有标题，不覆盖非空描述，也不生成图片。我已指派这项整理，请把20张全部完成，再按你添加的标签搜索验证可检索。每批最多8张，请分批完成。"}}}}))
	if err != nil {
		t.Fatal(err)
	}
	var run Run
	if err = json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	workErr := f.work(f.alice, run.ID)
	rows, err := f.db.Query(`SELECT tool_key,state,input::text,output::text FROM creative_agent_steps WHERE account_id='agent-a' AND run_id=$1 AND kind='tool' ORDER BY ordinal`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	viewed := map[string]bool{}
	verified := map[string]bool{}
	organized := map[string]bool{}
	for rows.Next() {
		var key, state, in, out string
		if err := rows.Scan(&key, &state, &in, &out); err != nil {
			_ = rows.Close() // Scan/Rows.Err is checked below.
			t.Fatal(err)
		}
		var invocation struct{ Arguments json.RawMessage }
		var envelope struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal([]byte(in), &invocation); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		if key == readAssetTool && state == "succeeded" {
			var result struct {
				Image string `json:"image_revision_id"`
			}
			if err := json.Unmarshal([]byte(envelope.Result), &result); err != nil {
				t.Fatal(err)
			}
			if result.Image != "" {
				viewed[result.Image] = true
				reads++
			}
		}
		if key == organizeAssetsTool && state == "succeeded" {
			var args struct {
				Items []struct {
					AssetID string `json:"asset_id"`
				} `json:"items"`
			}
			if err := json.Unmarshal(invocation.Arguments, &args); err != nil {
				t.Fatal(err)
			}
			for _, item := range args.Items {
				if !viewed[revisions[item.AssetID]] {
					t.Errorf("organized %s before viewing its image", item.AssetID)
				}
				organized[item.AssetID] = true
			}
		}
		if key == searchAssetsTool && state == "succeeded" {
			var args struct {
				TagIDs []string `json:"tag_ids"`
			}
			var result struct {
				Items []struct {
					AssetID string `json:"asset_id"`
				} `json:"items"`
			}
			if err := json.Unmarshal(invocation.Arguments, &args); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(envelope.Result), &result); err != nil {
				t.Fatal(err)
			}
			if len(args.TagIDs) > 0 {
				for _, item := range result.Items {
					if organized[item.AssetID] {
						verified[item.AssetID] = true
					}
				}
			}
		}
		t.Logf("tool=%s state=%s input=%s output=%s", key, state, in, out)
	}
	_ = rows.Close() // Scan/Rows.Err is checked below.
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var cost, tokens int64
	if err := f.db.QueryRow(`SELECT COALESCE(sum(booked_cost_micros),0) FROM llm_cost_positions WHERE account_id='agent-a'`).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COALESCE(sum(token_count),0) FROM llm_usage_measurements WHERE account_id='agent-a' AND cost_component='budget_total_tokens'`).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	t.Logf("20-image elapsed=%s read_calls=%d booked_cost_micros=%d currency=%s reported_total_tokens=%d", time.Since(start), reads, cost, currency, tokens)
	if workErr != nil {
		t.Fatal(workErr)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatal(got, err)
	}
	if len(viewed) != 20 || len(verified) != 20 {
		t.Fatalf("unique viewed=%d, tag search verified=%d; want 20 each", len(viewed), len(verified))
	}
	if n := f.count("creative_assets", `account_id='agent-a' AND kind='image' AND description<>'' AND id IN (SELECT asset_id FROM creative_asset_tags WHERE account_id='agent-a')`); n != 20 {
		t.Fatalf("organized %d of 20", n)
	}
}

func TestLiveAgentLibraryOverwriteRequiresConfirmation(t *testing.T) {
	f, _ := liveLibraryFixture(t)
	f.queue()
	revision := f.revision(f.alice, "文字参考")
	var asset string
	if err := f.db.QueryRow(`SELECT id FROM creative_assets WHERE account_id='agent-a' AND content_revision_id=$1`, revision).Scan(&asset); err != nil {
		t.Fatal(err)
	}
	var original strings.Builder
	for i := range 45 {
		fmt.Fprintf(&original, "第%02d组参考：午后窗边柔光，人物侧身看向窗外，浅色背景，保留自然肤色。\n", i+1)
	}
	old := original.String() + "完整原文末尾标记"
	if _, err := f.db.Exec(`UPDATE creative_assets SET title='覆盖确认参考',description=$1 WHERE account_id='agent-a' AND id=$2`, old, asset); err != nil {
		t.Fatal(err)
	}
	c := f.conversation(f.alice, f.canvasID)
	consent := libraryConsent(t, f, c.ID)
	receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{"conversation_id": c.ID, "model_key": "gpt-6.1-sol", "egress_consent_id": consent.ID, "instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{{Type: "text", Text: fmt.Sprintf("请整理资产库素材 %s，帮我优化它已有的标题和描述，让它更好检索。", asset)}}}}))
	if err != nil {
		t.Fatal(err)
	}
	var run Run
	if err = json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatal(got, err)
	}
	result, err := creativelibrary.GetAsset(t.Context(), f.alice, asset)
	if err != nil || result.Title != "覆盖确认参考" || result.Description != old {
		t.Fatal("unconfirmed overwrite", err)
	}
	rows, err := f.db.Query(`SELECT body::text FROM creative_agent_messages WHERE account_id='agent-a' AND run_id=$1 AND role='assistant' ORDER BY ordinal`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		var parsed Body
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		for _, block := range parsed.Blocks {
			if block.Type == "text" {
				answer.WriteString(block.Text)
			}
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("confirmation_answer=%s", answer.String())
	if !strings.Contains(answer.String(), old) || !strings.Contains(answer.String(), "确认") {
		t.Fatal("did not show full original and ask for confirmation")
	}
}

func liveLibraryRun(t *testing.T, f *fixture, conversation, consent, prompt string) string {
	t.Helper()
	receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{"conversation_id": conversation, "model_key": "gpt-6.1-sol", "egress_consent_id": consent, "instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{{Type: "text", Text: prompt}}}}))
	if err != nil {
		t.Fatal(err)
	}
	var run Run
	if err = json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatal(got, err)
	}
	var body string
	if err = f.db.QueryRow(`SELECT string_agg(body::text,E'\n' ORDER BY ordinal) FROM creative_agent_messages WHERE account_id='agent-a' AND run_id=$1 AND role='assistant'`, run.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	t.Logf("run=%s answer=%s", run.ID, body)
	return body
}

func TestLiveAgentLibrarySuggestionsRequireConfirmation(t *testing.T) {
	for _, mode := range []string{"creation", "save"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := liveLibraryFixture(t)
			f.queue()
			c := f.conversation(f.alice, f.canvasID)
			consent := libraryConsent(t, f, c.ID)
			prompt := "请使用资产库里的文字素材为我构思一张海报，只输出创作方案；如果发现资产库可以整理得更便于复用，也说说你的建议。"
			if mode == "creation" {
				f.revision(f.alice, "金色逆光、胶片颗粒、海边人像的参考文字。")
			} else {
				h := &runtimeHandler{service: f.service, scope: f.alice}
				if _, err := h.editCanvas(t.Context(), f.canvasID, "live-save-fixture", `{"actions":[{"type":"add_node","key":"text","node_type":"text","title":"海边人像","text":"金色逆光、胶片颗粒，适合海边人像的视觉参考。"}]}`); err != nil {
					t.Fatal(err)
				}
				prompt = "把当前画布中的文字素材存入资产库，标题、描述和归类请你根据内容拟定。"
			}
			before := f.count("creative_assets", `account_id='agent-a'`)
			answer := liveLibraryRun(t, f, c.ID, consent.ID, prompt)
			if n := f.count("creative_agent_library_calls", `account_id='agent-a'`); n != 0 {
				t.Fatalf("unconfirmed library write calls=%d", n)
			}
			if n := f.count("creative_assets", `account_id='agent-a'`); n != before {
				t.Fatal("wrote library before confirmation")
			}
			if mode == "save" {
				for _, field := range []string{"标题", "描述", "分组", "标签"} {
					if !strings.Contains(answer, field) {
						t.Errorf("save proposal missing %s", field)
					}
				}
				liveLibraryRun(t, f, c.ID, consent.ID, "同意，就按照你刚才列出的标题、描述、分组和标签存入资产库。")
				if n := f.count("creative_assets", `account_id='agent-a' AND description<>''`); n != 1 {
					t.Fatalf("confirmed save not completed: %d", n)
				}
			}
		})
	}
}
