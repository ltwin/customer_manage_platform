package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
)

// The skill store has its own error values, deliberately separate from the
// media domain's. A value the edge does not map falls through to the shared
// middleware as an unexpected error, which renders 500 and writes an error log
// line — so for the skill directory, where refusing across accounts is reported
// as absence, an unmapped ErrNotFound would turn every guess at an id into both
// a server fault and a log entry.
func TestASkillRefusalIsAnAnswerAndNotAServerFault(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{"不存在或不可见", creativeskill.ErrNotFound, 404, CodeNotFound},
		{"超出上限", creativeskill.ErrLimit, 413, "creative_skill_limit"},
		// Integrity faults are 5xx: on a read this means the stored object no
		// longer matches the version's own hash, which no client can act on.
		{"资源与声明不一致", creativeskill.ErrContentMismatch, 500, "creative_skill_content_mismatch"},
		{"对象存储不可读", creativeskill.ErrResourceUnavailable, 503, "creative_skill_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			creativeError(c, tc.err)
			if recorder.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.want, recorder.Body)
			}
			// The error must be rendered, not handed to the middleware: an entry
			// in c.Errors is exactly the 500-and-log path this test rules out.
			if len(c.Errors) != 0 {
				t.Fatalf("the refusal reached the unexpected-error path: %v", c.Errors)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tc.code {
				t.Fatalf("code=%q want=%q", body.Error.Code, tc.code)
			}
		})
	}
}

// A run's refusals are answers the photographer can act on, not server faults.
// Each one has to reach the edge through creativeError — an error added to the
// domain but not listed there falls through to the shared middleware, which
// renders 500 and writes a log line for something that was never a fault.
func TestARunRefusalIsAnAnswerAndNotAServerFault(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{"写槽位被占", creativeagent.BusyError{ActiveRunID: "ccrn_1"}, 429, "creative_agent_busy"},
		{"授权不覆盖", creativeagent.ErrEgressRequired, 403, "creative_egress_required"},
		{"授权已撤销", creativeagent.ErrConsentRevoked, 409, "creative_egress_revoked"},
		{"额度用尽", creativeagent.ErrBudgetExceeded, 429, "creative_llm_budget_exceeded"},
		{"模型不可用", creativeagent.ErrModelCapability, 422, "creative_model_capability_missing"},
		{"片段本部署跑不了", creativeagent.ErrUnsupportedSegment, 422, "creative_segment_unsupported"},
		{"运行状态不允许", creativeagent.ErrRunState, 409, "creative_run_state_conflict"},
		{"不能安全恢复", creativeagent.ErrRecoveryEvidence, 409, "creative_recovery_unavailable"},
		{"运行不存在", creativeagent.ErrNotFound, 404, CodeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			creativeError(c, tc.err)
			if recorder.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.want, recorder.Body)
			}
			if len(c.Errors) != 0 {
				t.Fatalf("the refusal reached the unexpected-error path: %v", c.Errors)
			}
			var body struct {
				Error struct {
					Code    string          `json:"code"`
					Details json.RawMessage `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tc.code {
				t.Fatalf("code=%q want=%q", body.Error.Code, tc.code)
			}
			if tc.code != "creative_agent_busy" {
				return
			}
			// "Busy" without saying which run is advice the photographer cannot
			// follow, so the identifier travels as typed detail rather than
			// being folded into the prose.
			var details struct {
				ActiveRunID string `json:"active_run_id"`
			}
			if err := json.Unmarshal(body.Error.Details, &details); err != nil {
				t.Fatal(err)
			}
			if details.ActiveRunID != "ccrn_1" {
				t.Fatalf("the refusal did not name the run holding the slot: %s", body.Error.Details)
			}
		})
	}
}
