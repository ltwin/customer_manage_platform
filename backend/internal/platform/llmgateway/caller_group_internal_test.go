package llmgateway

import (
	"slices"
	"strings"
	"testing"
)

// A caller group lock reads every request of a run, one per model step. With
// the prompt in each row it grew by the prompt size per step (about 160 KB, so
// half a second each against a remote development database).
func TestRequestViewColumnsLeaveOutThePayload(t *testing.T) {
	if slices.Contains(strings.Split(requestViewColumns, ","), "request_payload") {
		t.Fatal("requestViewColumns must not read request_payload")
	}
}
