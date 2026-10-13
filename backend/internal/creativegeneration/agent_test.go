package creativegeneration_test

import (
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
)

// 助手只能在结果真的放到节点上（或仍可能放上去）时说「已更新」。
func TestAgentGenerationAdoptedOnlyWhenTheNodeTakesTheResult(t *testing.T) {
	for _, tc := range []struct {
		state, apply string
		adopted      bool
	}{
		{"running", "pending", true},
		{"succeeded", "pending", true},
		{"succeeded", "applied", true},
		{"succeeded", "conflicted", false},
		{"succeeded", "discarded", false},
		{"succeeded", "expired", false},
		{"failed", "discarded", false},
	} {
		r := creativegeneration.AgentGenerationResult{State: tc.state, ApplyState: tc.apply}
		if r.Adopted() != tc.adopted {
			t.Errorf("%s/%s adopted=%v", tc.state, tc.apply, r.Adopted())
		}
	}
}
