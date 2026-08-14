package planshare_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
)

func TestTupleV1SegmentBoundariesAndNoCollision(t *testing.T) {
	a := planshare.TupleV1("gen|a", "shot_b")
	b := planshare.TupleV1("gen", "a|shot_b")
	if a == b {
		t.Fatalf("tuple collision across segment boundary")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0] != "gen|a" || parts[1] != "shot_b" {
		t.Fatalf("unexpected tuple decode: %#v", parts)
	}
}

func TestAnonymousFeedbackResourceConstructors(t *testing.T) {
	plan := planshare.AnonymousPlanFeedbackResource("plan_1", "gen_1")
	if plan.Kind() != "plan-share-plan-feedback" || plan.PrimaryID() != "plan_1" || plan.SecondaryID() != "gen_1" {
		t.Fatalf("plan resource: %#v", plan)
	}
	shot := planshare.AnonymousShotFeedbackResource("plan_1", "gen_1", "shot_1")
	if shot.Kind() != "plan-share-shot-feedback" || shot.PrimaryID() != "plan_1" {
		t.Fatalf("shot resource: %#v", shot)
	}
	if shot.SecondaryID() != planshare.TupleV1("gen_1", "shot_1") {
		t.Fatalf("shot secondary want tuple, got %q", shot.SecondaryID())
	}
	fb := planshare.FeedbackResource("plan_1", "sfb_1")
	if fb.Kind() != "plan-share-feedback" || fb.SecondaryID() != "sfb_1" {
		t.Fatalf("feedback resource: %#v", fb)
	}
}

func TestCanonicalAnonymousMutationFrameV1BytesStable(t *testing.T) {
	resource := planshare.AnonymousPlanFeedbackResource("plan_1", "gen_1")
	body := []byte(`{"author_display_name":"匿名","content":"hello","expected_projection_revision":3,"policy_version":"v1"}`)
	frame, meta, err := planshare.BuildCanonicalAnonymousMutationFrameV1Bytes(
		string(idempotency.OperationPlanShareFeedbackPlanCreate), resource, body,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(frame), `["planshare-anonymous-mutation-frame-v1"`) {
		t.Fatalf("frame prefix: %s", frame)
	}
	if len(meta.FrameHash) != 64 {
		t.Fatalf("frame hash length=%d", len(meta.FrameHash))
	}
	again, meta2, err := planshare.BuildCanonicalAnonymousMutationFrameV1Bytes(
		string(idempotency.OperationPlanShareFeedbackPlanCreate), resource, body,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame) != string(again) || meta.FrameHash != meta2.FrameHash {
		t.Fatalf("frame not stable")
	}
}
