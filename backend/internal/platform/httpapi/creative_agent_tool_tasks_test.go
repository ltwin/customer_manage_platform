package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

func TestCreativeAgentToolTaskRoutesAndProjections(t *testing.T) {
	f := newMCPFixture(t, false)
	conversation, run, step := "ccco_"+uuid.NewString(), "ccrn_"+uuid.NewString(), "ccst_"+uuid.NewString()
	handle, foreground, job, block := "cctask_"+uuid.NewString(), "cctask_"+uuid.NewString(), "csjob_"+uuid.NewString(), "ccbk_"+uuid.NewString()
	insert := func(query string, args ...any) {
		t.Helper()
		if _, err := f.db.ExecContext(t.Context(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	insert(`INSERT INTO creative_agent_conversations(id,account_id,project_id,canvas_id,title)
		VALUES($1,$2,'project',$3,'测试')`, conversation, testAcctID, f.canvasID)
	insert(`INSERT INTO creative_agent_runs(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,model_key,
		model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at)
		VALUES($1,$2,$3,$4,'message','consent','model','{}','running','token','v1','{}',now()+interval '1 hour')`, run, testAcctID, conversation, f.canvasID)
	insert(`INSERT INTO creative_agent_steps(id,account_id,run_id,ordinal,kind,state,execution_epoch,tool_key,input_hash,input)
		VALUES($1,$2,$3,1,'tool','succeeded',1,'write_model','fixture','{"arguments":{}}')`, step, testAcctID, run)
	insert(`INSERT INTO creative_scene_jobs(id,account_id,document_id,kind,request,deadline)
		VALUES($1,$2,'cwdoc_x','bake_model','{}',now()+interval '10 minutes')`, job, testAcctID)
	insert(`INSERT INTO creative_agent_tool_tasks(id,account_id,conversation_id,canvas_id,source_run_id,tool_step_id,tool_key,source_id,mode,model_key)
		VALUES($1,$2,$3,$4,$5,$6,'write_model',$7,'background','model'),
		      ($8,$2,$3,$4,$5,$9,'render_view','unused','foreground','model')`, handle, testAcctID, conversation, f.canvasID, run, step, job, foreground, "ccst_"+uuid.NewString())
	insert(`INSERT INTO creative_agent_blocks(id,account_id,run_id,ordinal,type,state,payload_version,source_key,hidden,payload,started_at,finished_at)
		VALUES($1,$2,$3,1,'tool','succeeded',1,$4,false,jsonb_build_object('step_id',$5::text,'tool_key','write_model','title','写入模型'),now(),now())`,
		block, testAcctID, run, "call:"+step, step)

	created := time.Now()
	post := func(target, action, bearer, key, payload string) (int, []byte) {
		t.Helper()
		body, err := json.Marshal(httpapi.CreativeAgentControlRequest{
			OperationId: uuid.MustParse(key), ClientCreatedAt: created,
		})
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		envelope["payload"] = json.RawMessage(payload)
		body, err = json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/api/v1/creative/agent-tool-tasks/"+target+"/"+action, strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		res, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, raw
	}
	for _, action := range []string{"detach", "cancel"} {
		if code, _ := post(handle, action, "invalid-token", uuid.NewString(), "{}"); code != 401 {
			t.Fatalf("%s with invalid credentials: %d", action, code)
		}
		code, raw := post(handle, action, f.otherWeb, uuid.NewString(), "{}")
		if code != 404 || !strings.Contains(string(raw), "会话、运行或授权不存在") {
			t.Fatalf("%s across account: %d %s", action, code, raw)
		}
		code, raw = post(foreground, action, f.web, uuid.NewString(), "{}")
		if code != 409 || !strings.Contains(string(raw), "creative_run_state_conflict") {
			t.Fatalf("%s foreground screenshot: %d %s", action, code, raw)
		}
	}
	if code, _ := post("invalid", "detach", f.web, uuid.NewString(), "{}"); code != 400 {
		t.Fatalf("invalid handle: %d", code)
	}
	if code, _ := post(handle, "detach", f.web, uuid.NewString(), `{"handle":"other"}`); code != 400 {
		t.Fatalf("client sent a target in payload: %d", code)
	}
	key := uuid.NewString()
	code, original := post(handle, "detach", f.web, key, "{}")
	if code != 200 || !strings.Contains(string(original), `"mode":"background"`) {
		t.Fatalf("detach replay of background task: %d %s", code, original)
	}
	if code, raw := post(foreground, "detach", f.web, key, "{}"); code != 409 || !strings.Contains(string(raw), "idempotency_conflict") {
		t.Fatalf("key used on a different handle: %d %s", code, raw)
	}
	cancelKey := uuid.NewString()
	code, raw := post(handle, "cancel", f.web, cancelKey, "{}")
	var task httpapi.CreativeAgentToolTask
	if code != 200 || json.Unmarshal(raw, &task) != nil || task.Handle != handle ||
		task.State != "cancelled" || task.FinishedAt == nil || task.Result == nil {
		t.Fatalf("cancel: %d %s", code, raw)
	}
	sameTask := func(left, right []byte) bool {
		var a, b httpapi.CreativeAgentToolTask
		return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
	}
	if code, repeated := post(handle, "cancel", f.web, cancelKey, "{}"); code != 200 || !sameTask(repeated, raw) {
		t.Fatalf("cancel replay: %d %s", code, repeated)
	}
	if code, replay := post(handle, "detach", f.web, key, "{}"); code != 200 || !sameTask(replay, original) {
		t.Fatalf("detach receipt after newer task revision: %d %s", code, replay)
	}

	base := "/api/v1/creative/"
	for _, path := range []string{"agent-runs/" + run, "conversations/" + conversation + "/messages"} {
		res := f.do(t, http.MethodGet, base+path, f.web, "", "")
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 || !strings.Contains(string(body), handle) || !strings.Contains(string(body), `"state":"cancelled"`) {
			t.Fatalf("projection %s: %d %s %v", path, res.StatusCode, body, err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.server.URL+base+"agent-runs/"+run+"/events?after_seq=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.web)
	res, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	scanner := bufio.NewScanner(res.Body)
	var patched, notified bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			patched = patched || strings.Contains(line, "block.updated") && strings.Contains(line, handle)
			notified = notified || strings.Contains(line, "background_tool.completed") && strings.Contains(line, handle)
		}
		if patched && notified {
			break
		}
	}
	if res.StatusCode != 200 || !patched || !notified {
		t.Fatalf("run events: status=%d patched=%t notified=%t err=%v", res.StatusCode, patched, notified, scanner.Err())
	}
}
