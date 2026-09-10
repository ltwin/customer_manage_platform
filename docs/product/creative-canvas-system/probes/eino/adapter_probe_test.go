// Eino v0.9.19 设计适配实验。真实ADK/PG，脚本化模型与最小业务表，非生产实现。
package einodesignprobe

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) {
	if os.Getenv("EINO_PROBE_CHILD") != "" {
		os.Exit(m.Run())
	}
	storetest.Main(m, func(string) error { return nil })
}
func setup(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := storetest.NewURL(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE probe_runs(account TEXT,run TEXT,epoch BIGINT,cancelled BOOLEAN DEFAULT false,PRIMARY KEY(account,run));
 CREATE TABLE probe_models(account TEXT,run TEXT,request TEXT,hash TEXT,payload JSONB,PRIMARY KEY(account,run,request));
 CREATE TABLE probe_receipts(account TEXT,run TEXT,operation TEXT,hash TEXT,result TEXT,PRIMARY KEY(account,run,operation));
 CREATE TABLE probe_effects(account TEXT,run TEXT,value TEXT,PRIMARY KEY(account,run));
 CREATE TABLE probe_checkpoints(account TEXT,id TEXT,payload BYTEA,PRIMARY KEY(account,id));
 CREATE TABLE probe_offloads(account TEXT,path TEXT,body TEXT,PRIMARY KEY(account,path));
 INSERT INTO probe_runs VALUES('a','run',1,false)`)
	if err != nil {
		t.Fatal(err)
	}
	return db, dsn
}
func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 模型适配器仅委派到注入的Gateway端口；测试端口由脚本函数/PG回放器实现。
type gatewayModel struct {
	complete func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error)
	tools    []*schema.ToolInfo
}

func (m *gatewayModel) WithTools(in []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	next := *m
	next.tools = append([]*schema.ToolInfo(nil), in...)
	return &next, nil
}
func (m *gatewayModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	o := model.GetCommonOptions(nil, opts...)
	ts := m.tools
	if o.Tools != nil {
		ts = o.Tools
	}
	return m.complete(ctx, in, ts)
}
func (m *gatewayModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, in, opts...)
	if e != nil {
		return nil, e
	}
	return schema.StreamReaderFromArray([]*schema.Message{v}), nil
}
func toolCall(name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{ID: "call-" + name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
}
func sawTool(in []*schema.Message, name string) bool {
	for _, m := range in {
		if m.Role == schema.Tool && m.ToolCallID == "call-"+name {
			return true
		}
	}
	return false
}
func messagesText(in []*schema.Message) string {
	var b strings.Builder
	for _, m := range in {
		b.WriteString(m.Content)
	}
	return b.String()
}
func newRunner(t *testing.T, m model.BaseChatModel, ts []tool.BaseTool, hs []adk.ChatModelAgentMiddleware, cp adk.CheckPointStore) *adk.Runner {
	t.Helper()
	a, e := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{Name: "creative", Instruction: "Use the declared tools for this probe.", Model: m, ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: ts, ExecuteSequentially: true}}, Handlers: hs, MaxIterations: 6})
	if e != nil {
		t.Fatal(e)
	}
	return adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: a, CheckPointStore: cp})
}
func drain(it *adk.AsyncIterator[*adk.AgentEvent]) (string, *adk.InterruptInfo, error) {
	var out string
	for {
		e, ok := it.Next()
		if !ok {
			break
		}
		if e.Err != nil {
			return out, nil, e.Err
		}
		if e.Action != nil && e.Action.Interrupted != nil {
			return out, e.Action.Interrupted, nil
		}
		if e.Output != nil && e.Output.MessageOutput != nil {
			m, err := e.Output.MessageOutput.GetMessage()
			if err != nil {
				return out, nil, err
			}
			if m.Role == schema.Assistant {
				out = m.Content
			}
		}
	}
	return out, nil, nil
}

// 两轮固定轨迹使用预先定义的logical request 1/2；不是生产通用步骤分配算法。
func replayGateway(db *sql.DB) *gatewayModel {
	return &gatewayModel{complete: func(ctx context.Context, in []*schema.Message, ts []*schema.ToolInfo) (*schema.Message, error) {
		key := "model-1"
		response := toolCall("write_text", `{"value":"persisted"}`)
		if sawTool(in, "write_text") {
			key = "model-2"
			response = schema.AssistantMessage("done", nil)
		}
		if len(ts) != 1 || ts[0].Name != "write_text" {
			return nil, errors.New("tool binding missing")
		}
		// hash固定业务输入轮廓，忽略Eino传输metadata；完整通用规范化不属于此探针。
		semantic := struct{ Request, Instruction string }{key, "write persisted"}
		raw, _ := json.Marshal(semantic)
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		var priorHash string
		var payload []byte
		e := db.QueryRowContext(ctx, `SELECT hash,payload FROM probe_models WHERE account='a' AND run='run' AND request=$1`, key).Scan(&priorHash, &payload)
		if e == nil {
			if priorHash != hash {
				return nil, errors.New("request conflict")
			}
			var v schema.Message
			e = json.Unmarshal(payload, &v)
			return &v, e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		payload, e = json.Marshal(response)
		if e != nil {
			return nil, e
		}
		_, e = db.ExecContext(ctx, `INSERT INTO probe_models VALUES('a','run',$1,$2,$3)`, key, hash, string(payload))
		return response, e
	}}
}

type pgCheckpoint struct {
	db      *sql.DB
	account string
	crash   bool
}

func (c *pgCheckpoint) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var b []byte
	e := c.db.QueryRowContext(ctx, `SELECT payload FROM probe_checkpoints WHERE account=$1 AND id=$2`, c.account, key).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, false, nil
	}
	return b, e == nil, e
}
func (c *pgCheckpoint) Set(ctx context.Context, key string, b []byte) error {
	if c.crash {
		os.Exit(71)
	} // 确定性故障点：工具效果已提交，checkpoint尚未写入，真实子进程退出。
	_, e := c.db.ExecContext(ctx, `INSERT INTO probe_checkpoints VALUES($1,$2,$3) ON CONFLICT(account,id) DO UPDATE SET payload=EXCLUDED.payload`, c.account, key, b)
	return e
}

type writeTool struct {
	db    *sql.DB
	epoch int
	mode  string
}

func (w *writeTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "write_text", Desc: "Persist one text value", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"value": {Type: schema.String, Required: true}})}, nil
}
func (w *writeTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	if compose.GetToolCallID(ctx) != "call-write_text" {
		return "", errors.New("unregistered tool call")
	}
	var in struct {
		Value string `json:"value"`
	}
	dec := json.NewDecoder(strings.NewReader(args))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&in); e != nil {
		return "", e
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(in.Value)))
	interrupted, _, _ := tool.GetInterruptState[string](ctx)
	if w.mode == "pause" && !interrupted {
		return "", tool.StatefulInterrupt(ctx, "confirm", args)
	}
	tx, e := w.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var epoch int
	var cancelled bool
	if e = tx.QueryRowContext(ctx, `SELECT epoch,cancelled FROM probe_runs WHERE account='a' AND run='run' FOR UPDATE`).Scan(&epoch, &cancelled); e != nil {
		return "", e
	}
	if cancelled || epoch != w.epoch {
		return "", errors.New("execution fenced")
	}
	var prior, result string
	e = tx.QueryRowContext(ctx, `SELECT hash,result FROM probe_receipts WHERE account='a' AND run='run' AND operation='tool-1'`).Scan(&prior, &result)
	if e == nil {
		if prior != hash {
			return "", errors.New("operation conflict")
		}
		return result, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO probe_effects VALUES('a','run',$1);`, in.Value); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO probe_receipts VALUES('a','run','tool-1',$1,'saved')`, hash); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	if w.mode == "crash" {
		return "", tool.StatefulInterrupt(ctx, "after commit", args)
	}
	return "saved", nil
}
func runChild(t *testing.T, dsn, mode string, wantExit int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProbeChild$", "-test.v")
	cmd.Env = append(os.Environ(), "EINO_PROBE_CHILD="+mode, "EINO_PROBE_DB="+dsn)
	out, err := cmd.CombinedOutput()
	if wantExit == 0 {
		if err != nil {
			t.Fatalf("child failed: %v\n%s", err, out)
		}
		return
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != wantExit {
		t.Fatalf("expected exit %d: %v\n%s", wantExit, err, out)
	}
}
func TestProbeChild(t *testing.T) {
	mode := os.Getenv("EINO_PROBE_CHILD")
	if mode == "" {
		t.Skip("subprocess-only helper")
	}
	db, e := sql.Open("pgx", os.Getenv("EINO_PROBE_DB"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var epoch int
	if e = db.QueryRow(`SELECT epoch FROM probe_runs WHERE account='a' AND run='run'`).Scan(&epoch); e != nil {
		t.Fatal(e)
	}
	toolMode := mode
	if mode == "resume" || mode == "cancelled" {
		toolMode = "pause"
	}
	r := newRunner(t, replayGateway(db), []tool.BaseTool{&writeTool{db, epoch, toolMode}}, nil, &pgCheckpoint{db, "a", mode == "crash"})
	var it *adk.AsyncIterator[*adk.AgentEvent]
	if mode == "resume" || mode == "cancelled" {
		it, e = r.Resume(context.Background(), "checkpoint")
		if e != nil {
			t.Fatal(e)
		}
	} else {
		it = r.Query(context.Background(), "write persisted", adk.WithCheckPointID("checkpoint"))
	}
	out, interrupt, e := drain(it)
	if mode == "cancelled" {
		if e == nil || !strings.Contains(e.Error(), "execution fenced") {
			t.Fatalf("expected fence: %v", e)
		}
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	if mode == "pause" {
		if interrupt == nil {
			t.Fatal("missing interruption")
		}
		return
	}
	if mode == "crash" {
		t.Fatal("crash hook not reached")
	}
	if interrupt != nil || out != "done" {
		t.Fatalf("unexpected final %q %v", out, interrupt)
	}
}
func TestCheckpointAcrossProcesses(t *testing.T) {
	db, dsn := setup(t)
	runChild(t, dsn, "pause", 0)
	if count(t, db, `SELECT count(*) FROM probe_checkpoints`) != 1 || count(t, db, `SELECT count(*) FROM probe_effects`) != 0 {
		t.Fatal("pause not persisted before effect")
	}
	other := &pgCheckpoint{db, "b", false}
	if _, ok, e := other.Get(context.Background(), "checkpoint"); e != nil || ok {
		t.Fatal("checkpoint crossed account")
	}
	runChild(t, dsn, "resume", 0)
	if count(t, db, `SELECT count(*) FROM probe_effects`) != 1 || count(t, db, `SELECT count(*) FROM probe_models`) != 2 {
		t.Fatal("resume repeated model/tool")
	}
}
func TestCommitBeforeCheckpointCrash(t *testing.T) {
	db, dsn := setup(t)
	runChild(t, dsn, "crash", 71)
	if count(t, db, `SELECT count(*) FROM probe_receipts`) != 1 || count(t, db, `SELECT count(*) FROM probe_checkpoints`) != 0 {
		t.Fatal("crash point not reached")
	}
	if _, e := db.Exec(`UPDATE probe_runs SET epoch=2 WHERE account='a' AND run='run'`); e != nil {
		t.Fatal(e)
	}
	runChild(t, dsn, "replay", 0)
	if count(t, db, `SELECT count(*) FROM probe_effects`) != 1 || count(t, db, `SELECT count(*) FROM probe_models`) != 2 {
		t.Fatal("recovery duplicated fixed trace")
	}
}
func TestCancelledResumeCannotWrite(t *testing.T) {
	db, dsn := setup(t)
	runChild(t, dsn, "pause", 0)
	if _, e := db.Exec(`UPDATE probe_runs SET cancelled=true,epoch=2 WHERE account='a' AND run='run'`); e != nil {
		t.Fatal(e)
	}
	runChild(t, dsn, "cancelled", 0)
	if count(t, db, `SELECT count(*) FROM probe_effects`) != 0 {
		t.Fatal("cancelled run wrote")
	}
}

type skillBackend struct{ gets int }

func (b *skillBackend) List(context.Context) ([]skill.FrontMatter, error) {
	return []skill.FrontMatter{{Name: "reference-direction", Description: "Compare visual references"}}, nil
}
func (b *skillBackend) Get(_ context.Context, name string) (skill.Skill, error) {
	b.gets++
	if name != "reference-direction" {
		return skill.Skill{}, errors.New("skill unavailable")
	}
	return skill.Skill{FrontMatter: skill.FrontMatter{Name: name}, Content: "PINNED_SKILL_BODY_v1", BaseDirectory: "/virtual/skills/v1"}, nil
}
func TestSkillProgressiveLoading(t *testing.T) {
	b := &skillBackend{}
	h, e := skill.NewMiddleware(context.Background(), &skill.Config{Backend: b})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	m := &gatewayModel{complete: func(_ context.Context, in []*schema.Message, ts []*schema.ToolInfo) (*schema.Message, error) {
		calls++
		txt := messagesText(in)
		if calls == 1 {
			if strings.Contains(txt, "PINNED_SKILL_BODY") || b.gets != 0 {
				return nil, errors.New("skill body loaded eagerly")
			}
			if len(ts) != 1 || !strings.Contains(ts[0].Desc, "Compare visual references") {
				return nil, errors.New("skill metadata missing")
			}
			return toolCall("skill", `{"skill":"reference-direction"}`), nil
		}
		if !strings.Contains(txt, "PINNED_SKILL_BODY_v1") {
			return nil, errors.New("skill activation missing")
		}
		return schema.AssistantMessage("loaded", nil), nil
	}}
	out, interrupt, e := drain(newRunner(t, m, nil, []adk.ChatModelAgentMiddleware{h}, nil).Query(context.Background(), "compare references"))
	if e != nil || interrupt != nil || out != "loaded" || b.gets != 1 || calls != 2 {
		t.Fatalf("skill probe %q gets=%d calls=%d err=%v", out, b.gets, calls, e)
	}
}
func TestSummarizationUsesInjectedModel(t *testing.T) {
	summaries, mainCalls := 0, 0
	summary := &gatewayModel{complete: func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
		summaries++
		return schema.AssistantMessage("SUMMARY_V1", nil), nil
	}}
	h, e := summarization.New(context.Background(), &summarization.Config{Model: summary, Trigger: &summarization.TriggerCondition{ContextTokens: 1}, TokenCounter: func(context.Context, *summarization.TokenCounterInput) (int, error) { return 100, nil }, Finalize: func(_ context.Context, _ []*schema.Message, m *schema.Message) ([]*schema.Message, error) {
		return []*schema.Message{schema.UserMessage(m.Content)}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	main := &gatewayModel{complete: func(_ context.Context, in []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		mainCalls++
		if !strings.Contains(messagesText(in), "SUMMARY_V1") {
			return nil, errors.New("summary not used")
		}
		return schema.AssistantMessage("answer", nil), nil
	}}
	_, _, e = drain(newRunner(t, main, nil, []adk.ChatModelAgentMiddleware{h}, nil).Query(context.Background(), "old long history"))
	if e != nil || summaries != 1 || mainCalls != 1 {
		t.Fatalf("summary calls=%d main=%d err=%v", summaries, mainCalls, e)
	}
}

type offloadStore struct{ db *sql.DB }

func (b *offloadStore) Write(ctx context.Context, r *filesystem.WriteRequest) error {
	_, e := b.db.ExecContext(ctx, `INSERT INTO probe_offloads VALUES('a',$1,$2)`, r.FilePath, r.Content)
	return e
}
func TestReductionPreservesFullToolResult(t *testing.T) {
	db, _ := setup(t)
	large := strings.Repeat("PRIVATE_RESULT_", 100)
	h, e := reduction.New(context.Background(), &reduction.Config{Backend: &offloadStore{db}, SkipClear: true, MaxLengthForTrunc: 20, RootDir: "/virtual/run-a", ReadFileToolName: "ReadRunResult"})
	if e != nil {
		t.Fatal(e)
	}
	ts, e := utils.InferTool("read_large", "Read bounded fixture", func(context.Context, *struct{}) (string, error) { return large, nil })
	if e != nil {
		t.Fatal(e)
	}
	m := &gatewayModel{complete: func(_ context.Context, in []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		if !sawTool(in, "read_large") {
			return toolCall("read_large", "{}"), nil
		}
		if strings.Contains(messagesText(in), large) {
			return nil, errors.New("full tool output remained in context")
		}
		return schema.AssistantMessage("reduced", nil), nil
	}}
	out, _, e := drain(newRunner(t, m, []tool.BaseTool{ts}, []adk.ChatModelAgentMiddleware{h}, nil).Query(context.Background(), "read result"))
	if e != nil || out != "reduced" {
		t.Fatalf("reduction %q %v", out, e)
	}
	var saved string
	if e = db.QueryRow(`SELECT body FROM probe_offloads WHERE account='a'`).Scan(&saved); e != nil || saved != large {
		t.Fatalf("offload lost data %v", e)
	}
}
func TestUnknownModelErrorNotAutomaticallyRetried(t *testing.T) {
	calls := 0
	m := &gatewayModel{complete: func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
		calls++
		return nil, errors.New("gateway_result_unknown")
	}}
	_, _, e := drain(newRunner(t, m, nil, nil, nil).Query(context.Background(), "call"))
	if e == nil || calls != 1 {
		t.Fatalf("unknown retried: %d %v", calls, e)
	}
}
func TestAgentAsToolIsolatesInput(t *testing.T) {
	childCalls := 0
	childModel := &gatewayModel{complete: func(_ context.Context, in []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		childCalls++
		s := messagesText(in)
		if strings.Contains(s, "PARENT_PRIVATE") || !strings.Contains(s, "bounded task") {
			return nil, errors.New("wrong child context")
		}
		return schema.AssistantMessage("child result", nil), nil
	}}
	child, e := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{Name: "researcher", Description: "Research a bounded task", Model: childModel})
	if e != nil {
		t.Fatal(e)
	}
	parent := &gatewayModel{complete: func(_ context.Context, in []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
		if !sawTool(in, "researcher") {
			return toolCall("researcher", `{"request":"bounded task"}`), nil
		}
		if !strings.Contains(messagesText(in), "child result") {
			return nil, errors.New("child result missing")
		}
		return schema.AssistantMessage("combined", nil), nil
	}}
	out, _, e := drain(newRunner(t, parent, []tool.BaseTool{adk.NewAgentTool(context.Background(), child)}, nil, nil).Query(context.Background(), "PARENT_PRIVATE"))
	if e != nil || out != "combined" || childCalls != 1 {
		t.Fatalf("agent tool %q %d %v", out, childCalls, e)
	}
}

var _ model.ToolCallingChatModel = (*gatewayModel)(nil)
var _ adk.CheckPointStore = (*pgCheckpoint)(nil)
var _ tool.InvokableTool = (*writeTool)(nil)
