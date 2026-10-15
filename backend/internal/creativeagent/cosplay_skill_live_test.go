package creativeagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/webfetch"
)

// Explicit opt-in only: real model, real Firecrawl, the repository's own
// cosplay skill package. It spends chat tokens and search credits and writes
// one transcript per case for the V6-081 walkthrough record. Generation is not
// involved: these cases stop before any image is made.
//
//	CREATIVE_AGENT_LIVE=1 CREATIVE_SKILL_LIVE_OUT=<dir> go test ./internal/creativeagent/ -run TestLiveCosplaySkill -v
func TestLiveCosplaySkill(t *testing.T) {
	out := os.Getenv("CREATIVE_SKILL_LIVE_OUT")
	if os.Getenv("CREATIVE_AGENT_LIVE") != "1" || strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY")) == "" || out == "" {
		t.Skip("explicit live acceptance not enabled")
	}
	fetcher, err := webfetch.FromEnv(os.Getenv)
	if err != nil || fetcher == nil {
		t.Skipf("web tools are not configured: %v", err)
	}
	modelKey := os.Getenv("CREATIVE_SKILL_LIVE_MODEL")
	if modelKey == "" {
		modelKey = "gpt-6.1-sol"
	}
	cases := []struct{ name, prompt string }{
		{"hot", "芙宁娜，想拍一组合成片，交付 9 张。"},
		{"cold", "古剑奇谭三的北洛，外景，想拍得和别人不一样。"},
		{"ambiguous", "想拍阿尔托莉雅。"},
		{"hot-ip", "芙宁娜，合成片。这次只做 IP 拆解，完整写出来。"},
		{"cold-ip", "古剑奇谭三的北洛，外景。这次只做 IP 拆解，完整写出来。"},
	}
	if only := os.Getenv("CREATIVE_SKILL_LIVE_CASE"); only != "" {
		kept := cases[:0]
		for _, c := range cases {
			if c.name == only {
				kept = append(kept, c)
			}
		}
		cases = kept
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			defaults, err := llmgateway.DefaultCatalog(nil)
			if err != nil {
				t.Fatal(err)
			}
			model, err := defaults.Model(modelKey)
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := llmgateway.NewCatalog("cosplay-skill-live", model.Price.Currency, []llmgateway.ModelConfig{model})
			if err != nil {
				t.Fatal(err)
			}
			gateway, err := llmgateway.Build(llmgateway.Options{Catalog: catalog, MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 3_000_000, Timeout: 3 * time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			if f.service, err = NewService(gateway, catalog, f.skills); err != nil {
				t.Fatal(err)
			}
			f.service.SetWebFetcher(fetcher)
			f.queue()
			if _, err = f.skills.SyncPlatform(t.Context(), os.DirFS("../../../deploy/creative-skills")); err != nil {
				t.Fatal(err)
			}
			view, err := f.service.Catalog(t.Context(), f.alice)
			if err != nil {
				t.Fatal(err)
			}
			var skill SkillEntry
			for _, e := range view.Skills {
				if e.Slug == "cosplay-shoot-planning" {
					skill = e
				}
			}
			if !skill.Available {
				t.Fatalf("the cosplay skill is not runnable here: %+v", skill)
			}
			conversation := f.conversation(f.alice, f.canvasID)
			// What the panel grants by default: text, image previews and canvas metadata.
			consent := contextConsent(t, f, conversation.ID)
			receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{
				"conversation_id": conversation.ID, "model_key": modelKey, "egress_consent_id": consent.ID,
				"instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{
					{Type: "skill_ref", SkillID: skill.SkillID, SkillVersionID: skill.VersionID},
					{Type: "text", Text: c.prompt},
				}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			var run Run
			if err = json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			workErr := f.work(f.alice, run.ID)
			got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(out, "cosplay-"+c.name+".md")
			if err = os.WriteFile(path, []byte(cosplayTranscript(t, f, run.ID, modelKey, c.prompt, got, time.Since(started))), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("transcript %s", path)
			if workErr != nil || got.State != RunSucceeded {
				t.Fatalf("live run state=%s error=%s err=%v", got.State, got.ErrorCode, workErr)
			}
		})
	}
}

// cosplayTranscript lays out what the walkthrough reviewer reads: every tool
// call in order with its arguments and a bounded slice of its result, then the
// reply the photographer would see.
func cosplayTranscript(t *testing.T, f *fixture, runID, modelKey, prompt string, run Run, took time.Duration) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n- 模型：%s\n- 状态：%s %s\n- 用时：%s\n\n## 输入\n\n%s\n\n## 工具调用\n\n", prompt, modelKey, run.State, run.ErrorCode, took.Round(time.Second), prompt)
	rows, err := f.db.Query(`SELECT ordinal, attempt, tool_key, state, input::text, COALESCE(output::text,''), COALESCE(error_code,'')
		FROM creative_agent_steps WHERE run_id=$1 AND kind='tool' ORDER BY ordinal, attempt`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var ordinal, attempt int
		var tool, state, input, output, code string
		if err := rows.Scan(&ordinal, &attempt, &tool, &state, &input, &output, &code); err != nil {
			t.Fatal(err)
		}
		n++
		fmt.Fprintf(&b, "%d. `%s` %s %s\n   - 参数：`%s`\n   - 结果：%s\n", n, tool, state, code, clipText(input, 400), clipText(strings.ReplaceAll(output, "\n", " "), 600))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// Every model call with its route outcome, so a run that ends in
	// providers_exhausted says which failure used the attempts up.
	b.WriteString("\n## 模型调用\n\n")
	calls, err := f.db.Query(`SELECT to_char(created_at,'HH24:MI:SS'), to_char(updated_at,'HH24:MI:SS'), state, COALESCE(failure_class,''), COALESCE(failure_http_status,0),
		model_snapshot->>'deployment_key', COALESCE(octet_length(request_payload::text),0)
		FROM llm_requests WHERE caller_group_id=$1 ORDER BY created_at`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = calls.Close() }()
	for calls.Next() {
		var started, ended, state, class, deployment string
		var status, size int
		if err := calls.Scan(&started, &ended, &state, &class, &status, &deployment, &size); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "- %s–%s %s %s %s %d · 请求体 %d KB\n", started, ended, deployment, state, class, status, size>>10)
	}
	if err := calls.Err(); err != nil {
		t.Fatal(err)
	}
	b.WriteString("\n## 回复\n\n")
	var body string
	if err := f.db.QueryRow(`SELECT body::text FROM creative_agent_messages WHERE run_id=$1 AND role='assistant' ORDER BY ordinal DESC LIMIT 1`, runID).Scan(&body); err != nil {
		fmt.Fprintf(&b, "（没有回复：%v）\n", err)
		return b.String()
	}
	var reply Body
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	for _, block := range reply.Blocks {
		b.WriteString(block.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
