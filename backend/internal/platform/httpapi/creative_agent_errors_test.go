package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
