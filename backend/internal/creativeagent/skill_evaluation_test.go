package creativeagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/webfetch"
)

// Behavior assertions are deterministic. Semantic quality remains explicitly
// unreviewed until a person applies the rubric to the retained transcript.
type skillEvalCase struct {
	SeedReferences bool     `json:"seed_references"`
	ID             string   `json:"id"`
	Slug           string   `json:"skill"`
	Prompt         string   `json:"prompt"`
	Followup       string   `json:"followup,omitempty"`
	Resources      []string `json:"required_resources"`
	Tools          []string `json:"required_tools"`
	Rubric         []string `json:"quality_rubric"`
}

func skillEvalCases() []skillEvalCase {
	return []skillEvalCase{
		{ID: "cosplay-discovery", Slug: "cosplay-shoot-planning", Prompt: "芙宁娜，合成片，交付 9 张。请先调研并给我创意方向。", Resources: []string{"references/ip-breakdown.md", "references/creative-direction.md"}, Tools: []string{searchWebTool, readWebpageTool}, Rubric: []string{"引用资料实际支持角色结论", "给出 2–3 个方向并等待选择", "遵守 2+4+3，不提前生成"}},
		{ID: "cosplay-ambiguity", Slug: "cosplay-shoot-planning", Prompt: "我想拍阿尔托莉雅。", Rubric: []string{"确认作品、版本或形态，不擅自选定角色版本", "在身份明确前不虚构调研结果"}},
		{ID: "cosplay-missing-evidence", Slug: "cosplay-shoot-planning", Prompt: "给我的原创角色「雾灯拾贝人」做角色调研，没有公开设定。", Rubric: []string{"明确标注资料缺口", "请求设定或说明假设，不伪造来源"}},
		{ID: "reference-discovery", SeedReferences: true, Slug: "reference-direction", Prompt: "按参考整理的方法，为森系人像从我授权的个人库找 3 张参考，放在当前画布右侧。库里不足就说明缺口。", Resources: []string{"references/comparison-checklist.md"}, Tools: []string{searchAssetsTool, readCanvasTool}, Rubric: []string{"明确检索范围、主题和放置区域", "不编造素材；数量不足如实报告"}},
		{ID: "reference-empty-library", Slug: "reference-direction", Prompt: "从授权个人库找森系人像参考，放在画布右侧；不足请说明缺口，不要生成。", Tools: []string{searchAssetsTool}, Rubric: []string{"库为空时说明没有候选", "不虚构参考、比较结果或画布交付"}},
		{ID: "reference-clarification", Slug: "reference-direction", Prompt: "帮我整理参考素材。", Rubric: []string{"询问缺失的主题、范围或放置区域", "未明确范围前不擅自修改画布"}},
		{ID: "reference-continuation", SeedReferences: true, Slug: "reference-direction", Prompt: "按参考整理方法，为森系人像从授权的个人库找参考，放在当前画布右侧。", Followup: "沿用刚才的方法，再比较一次候选；重新核对比较清单。", Resources: []string{"references/comparison-checklist.md"}, Rubric: []string{"沿用原固定版本及其参考清单", "不丢失此前限定的范围"}},
	}
}

type skillEvalEvidence struct {
	RunID           string            `json:"run_id"`
	ExpectedVersion string            `json:"expected_version"`
	RunState        string            `json:"run_state"`
	Activated       bool              `json:"activated_expected_version"`
	SuccessfulTools map[string]int    `json:"successful_tools"`
	Resources       map[string]bool   `json:"resources"`
	ToolCalls       int               `json:"tool_calls"`
	GenerationCalls int               `json:"generation_calls"`
	Usage           []json.RawMessage `json:"usage_evidence"`
}
type skillEvalReport struct {
	Rounds           []skillEvalEvidence `json:"rounds"`
	Case             skillEvalCase       `json:"case"`
	Evidence         skillEvalEvidence   `json:"evidence"`
	BehaviorFailures []string            `json:"behavior_failures"`
	QualityStatus    string              `json:"quality_status"`
}

func evaluateSkillCase(c skillEvalCase, e skillEvalEvidence) skillEvalReport {
	report := skillEvalReport{Case: c, Evidence: e, Rounds: []skillEvalEvidence{e}, BehaviorFailures: []string{}, QualityStatus: "unreviewed"}
	if e.RunState != RunSucceeded {
		report.BehaviorFailures = append(report.BehaviorFailures, "运行未成功")
	}
	if !e.Activated {
		report.BehaviorFailures = append(report.BehaviorFailures, "没有激活预期的固定版本")
	}
	if e.GenerationCalls > 0 {
		report.BehaviorFailures = append(report.BehaviorFailures, "未获生成指令却调用生成")
	}
	for _, p := range c.Resources {
		if !e.Resources[p] {
			report.BehaviorFailures = append(report.BehaviorFailures, "未成功读取资源："+p)
		}
	}
	for _, name := range c.Tools {
		if e.SuccessfulTools[name] == 0 {
			report.BehaviorFailures = append(report.BehaviorFailures, "未成功调用工具："+name)
		}
	}
	return report
}
func evaluateSkillRounds(c skillEvalCase, rounds []skillEvalEvidence) skillEvalReport {
	report := evaluateSkillCase(c, rounds[len(rounds)-1])
	report.Rounds = rounds
	if c.Followup != "" {
		if len(rounds) != 2 || !rounds[0].Activated || rounds[0].RunState != RunSucceeded || rounds[0].ExpectedVersion != rounds[len(rounds)-1].ExpectedVersion {
			report.BehaviorFailures = append(report.BehaviorFailures, "没有完成首轮激活并续用同一固定版本")
		}
		// A method stays loaded across turns (shoot-plan design §6.6).
		if rounds[len(rounds)-1].SuccessfulTools[loadSkillTool] > 0 {
			report.BehaviorFailures = append(report.BehaviorFailures, "续用轮重复加载了已加载的方法")
		}
		for _, e := range rounds[:len(rounds)-1] {
			if e.GenerationCalls > 0 {
				report.BehaviorFailures = append(report.BehaviorFailures, "首轮未获生成指令却调用生成")
			}
		}
	}
	return report
}

func seedSkillEvaluationReferences(t *testing.T, f *fixture) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, color.RGBA{R: 50, G: uint8(100 + x*3), B: 70, A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	f.service.SetImageReader(lookupLiveImage{data: data.Bytes()})
	for i := range 3 {
		id := f.mediaRevision(f.alice, "image", "image/png")
		var asset string
		if err := f.db.QueryRow(`SELECT id FROM creative_assets WHERE account_id=$1 AND content_revision_id=$2`, f.alice.AccountID(), id).Scan(&asset); err != nil {
			t.Fatal(err)
		}
		title := fmt.Sprintf("森系人像绿色配色参考%d", i+1)
		description := "测试用绿色渐变色块，仅可用于配色比较，不是真实人像照片。"
		if _, err := creativelibrary.Metadata(t.Context(), f.alice, command(t, creativelibrary.MetadataInput{ID: asset, ExpectedRevision: 1, Title: &title, Description: &description})); err != nil {
			t.Fatal(err)
		}
	}
}

func skillEvidence(t *testing.T, f *fixture, run Run, version string) skillEvalEvidence {
	t.Helper()
	e := skillEvalEvidence{RunID: run.ID, ExpectedVersion: version, RunState: run.State, SuccessfulTools: map[string]int{}, Resources: map[string]bool{}, Usage: []json.RawMessage{}}
	e.Activated = f.count("creative_run_skills", "run_id=$1 AND skill_version_id=$2 AND activated_at IS NOT NULL", run.ID, version) > 0
	rows, err := f.db.Query(`SELECT tool_key,state,input FROM creative_agent_steps WHERE account_id=$1 AND run_id=$2 AND kind='tool' ORDER BY ordinal`, f.alice.AccountID(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, state string
		var raw []byte
		if err := rows.Scan(&name, &state, &raw); err != nil {
			_ = rows.Close() // Read-only rows; Scan/Err is checked.
			t.Fatal(err)
		}
		e.ToolCalls++
		if name == generateNodeTool {
			e.GenerationCalls++
		}
		if state != "succeeded" {
			continue
		}
		e.SuccessfulTools[name]++
		if name == readSkillTool {
			var call struct {
				Arguments struct {
					Version string `json:"skill_version_id"`
					Path    string `json:"path"`
				} `json:"arguments"`
			}
			if err := json.Unmarshal(raw, &call); err != nil {
				_ = rows.Close() // Read-only rows; Scan/Err is checked.
				t.Fatal(err)
			}
			if call.Arguments.Version == version {
				e.Resources[call.Arguments.Path] = true
			}
		}
	}
	err = rows.Err()
	_ = rows.Close() // Read-only rows; Scan/Err is checked.
	if err != nil {
		t.Fatal(err)
	}
	// Preserve the provider's evidence including unknown dimensions and revisions;
	// do not fabricate totals by summing superseded measurements.
	usage, err := f.db.Query(`SELECT m.usage_dimensions FROM llm_usage_measurements m JOIN llm_attempts a ON a.account_id=m.account_id AND a.id=m.attempt_id JOIN llm_requests r ON r.account_id=a.account_id AND r.id=a.request_id WHERE r.account_id=$1 AND r.caller_group_id=$2 ORDER BY m.created_at`, f.alice.AccountID(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for usage.Next() {
		var raw []byte
		if err := usage.Scan(&raw); err != nil {
			_ = usage.Close() // Read-only rows; Scan/Err is checked.
			t.Fatal(err)
		}
		e.Usage = append(e.Usage, json.RawMessage(raw))
	}
	err = usage.Err()
	_ = usage.Close() // Read-only rows; Scan/Err is checked.
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestSkillEvaluationDoesNotConfuseRunSuccessWithQuality(t *testing.T) {
	for _, c := range skillEvalCases() {
		t.Run(c.ID, func(t *testing.T) {
			e := skillEvalEvidence{RunState: RunSucceeded, SuccessfulTools: map[string]int{}, Resources: map[string]bool{}}
			if len(evaluateSkillCase(c, e).BehaviorFailures) == 0 {
				t.Fatal("success without activation passed")
			}
			e.Activated = true
			for _, p := range c.Resources {
				e.Resources[p] = true
			}
			for _, name := range c.Tools {
				e.SuccessfulTools[name] = 1
			}
			r := evaluateSkillCase(c, e)
			if len(r.BehaviorFailures) != 0 || r.QualityStatus != "unreviewed" {
				t.Fatalf("unexpected report: %+v", r)
			}
			e.GenerationCalls = 1
			if len(evaluateSkillCase(c, e).BehaviorFailures) == 0 {
				t.Fatal("unrequested generation passed")
			}
		})
	}
}

// Requires both the existing live opt-in and this evaluation-specific switch.
// Only synthetic test accounts are used; no image generation is configured.
func TestLiveSkillEvaluation(t *testing.T) {
	if os.Getenv("CREATIVE_SKILL_EVAL_LIVE") != "1" {
		t.Skip("explicit skill quality evaluation not enabled")
	}
	out := os.Getenv("CREATIVE_SKILL_LIVE_OUT")
	if out == "" {
		t.Fatal("CREATIVE_SKILL_LIVE_OUT is required")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range skillEvalCases() {
		t.Run(c.ID, func(t *testing.T) {
			f, _ := liveLibraryFixture(t)
			fetcher, err := webfetch.FromEnv(os.Getenv)
			if err != nil || fetcher == nil {
				t.Fatal("web provider required for complete skill evaluation", err)
			}
			f.service.SetWebFetcher(fetcher)
			f.queue()
			if c.SeedReferences {
				seedSkillEvaluationReferences(t, f)
			}
			if _, err := f.skills.SyncPlatform(t.Context(), os.DirFS("../../../deploy/creative-skills")); err != nil {
				t.Fatal(err)
			}
			page, err := f.service.ListSkills(t.Context(), f.alice, c.Slug, "", 20)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("skill missing: %v", err)
			}
			version := page.Items[0].VersionID
			conversation := f.conversation(f.alice, f.canvasID)
			consent := libraryConsent(t, f, conversation.ID)
			execute := func(prompt string) Run {
				receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{"conversation_id": conversation.ID, "model_key": "gpt-6.1-sol", "egress_consent_id": consent.ID, "instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{{Type: "text", Text: prompt}}}}))
				if err != nil {
					t.Fatal(err)
				}
				var run Run
				if err := json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
					t.Fatal(err)
				}
				if err := f.work(f.alice, run.ID); err != nil {
					t.Fatal(err)
				}
				got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			var rounds []skillEvalEvidence
			transcript := ""
			prompts := []string{c.Prompt}
			if c.Followup != "" {
				prompts = append(prompts, c.Followup)
			}
			for i, prompt := range prompts {
				started := time.Now()
				run := execute(prompt)
				rounds = append(rounds, skillEvidence(t, f, run, version))
				transcript += fmt.Sprintf("\n# Round %d\n", i+1) + cosplayTranscript(t, f, run.ID, "gpt-6.1-sol", prompt, run, time.Since(started))
			}
			report := evaluateSkillRounds(c, rounds)
			raw, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, c.ID+".json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			transcript += fmt.Sprintf("\n质量状态：%s。按 JSON 中 quality_rubric 逐项复核；运行成功不代表业务质量通过。\n", report.QualityStatus)
			if err := os.WriteFile(filepath.Join(out, c.ID+".md"), []byte(transcript), 0o644); err != nil {
				t.Fatal(err)
			}
			if len(report.BehaviorFailures) > 0 {
				t.Fatalf("behavior checks: %v", report.BehaviorFailures)
			}
		})
	}
}

func TestSkillContinuationRequiresBothRounds(t *testing.T) {
	c := skillEvalCase{Followup: "continue"}
	first := skillEvalEvidence{RunState: RunSucceeded, ExpectedVersion: "v1"}
	second := skillEvalEvidence{RunState: RunSucceeded, ExpectedVersion: "v1", Activated: true}
	if len(evaluateSkillRounds(c, []skillEvalEvidence{first, second}).BehaviorFailures) == 0 {
		t.Fatal("first load in second round counted as continuation")
	}
	first.Activated = true
	if len(evaluateSkillRounds(c, []skillEvalEvidence{first, second}).BehaviorFailures) != 0 {
		t.Fatal("valid continuation failed")
	}
	second.ExpectedVersion = "v2"
	if len(evaluateSkillRounds(c, []skillEvalEvidence{first, second}).BehaviorFailures) == 0 {
		t.Fatal("version changed")
	}
	second.ExpectedVersion = "v1"
	second.SuccessfulTools = map[string]int{loadSkillTool: 1}
	if len(evaluateSkillRounds(c, []skillEvalEvidence{first, second}).BehaviorFailures) == 0 {
		t.Fatal("reload in the follow-up round counted as continuation")
	}
}
