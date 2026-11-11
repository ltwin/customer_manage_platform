package creativegeneration

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestCharacterPolicyDiagnosticsUseFailedAttachmentAndRouteLimit(t *testing.T) {
	card := &creativecanvas.GenerationCharacter{Name: "云", Images: [2]int{1, 2}, ViewCount: 2}
	refs := []gw.GenerationReference{
		{Kind: gw.GenerationText}, {Kind: gw.GenerationImage, RevisionID: "first"},
		{Kind: gw.GenerationImage, RevisionID: "second"}, {Kind: gw.GenerationImage, RevisionID: "plain"},
	}
	index1, index2 := 1, 2
	bytes := gw.GenerationValueRef{Source: "references", Kind: gw.GenerationImage, Aggregate: "each", Field: "bytes"}
	issues := []gw.GenerationRuleIssue{{Code: "constraint_failed", Observations: []gw.GenerationRuleObservation{
		{Ref: bytes, Index: &index1, State: "known", Actual: json.RawMessage(`10`), Expected: []json.RawMessage{json.RawMessage(`20`)}},
		{Ref: bytes, Index: &index2, State: "known", Actual: json.RawMessage(`30`), Expected: []json.RawMessage{json.RawMessage(`20`)}},
	}}}
	var got *creativecanvas.CharacterGenerationError
	if err := characterPolicyIssue([]*creativecanvas.GenerationCharacter{card}, refs, issues); !errors.As(err, &got) || got.ImageIndex != 2 || got.RevisionID != "second" {
		t.Fatalf("wrong offending image: %+v, %v", got, err)
	}
	issues[0].Observations = []gw.GenerationRuleObservation{{
		Ref:   gw.GenerationValueRef{Source: "references", Kind: gw.GenerationImage, Aggregate: "count"},
		State: "known", Actual: json.RawMessage(`3`), Expected: []json.RawMessage{json.RawMessage(`2`)},
	}}
	if err := characterPolicyIssue([]*creativecanvas.GenerationCharacter{card}, refs, issues); !errors.As(err, &got) || got.Limit != 2 || got.Total != 3 {
		t.Fatalf("wrong limit: %+v, %v", got, err)
	}
}
