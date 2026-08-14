package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
)

func TestBearerFeedbackPageAPIAllowlistGolden(t *testing.T) {
	shotID := "shot_allowlist_1"
	disposedAt := time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)
	page := planshare.FeedbackManagementPageV1{
		Items: []planshare.FeedbackManagementItemV1{{
			FeedbackID:        "fb_1",
			AuthorDisplayName: "客户甲",
			Content:           "妆造再柔一点",
			Disposition:       planshare.FeedbackDispositionAdopted,
			DispositionAt:     &disposedAt,
			Revision:          2,
			CreatedAt:         disposedAt.Add(-time.Minute),
			Target: planshare.FeedbackTargetV1{
				Kind:   planshare.FeedbackTargetShot,
				ShotID: &shotID,
			},
			DeepLinkTarget: planshare.FeedbackDeepLinkTargetV1{
				Kind:   planshare.DeepLinkShot,
				ShotID: &shotID,
			},
		}},
		NextCursor: strPtr("cursor-1"),
	}

	raw, err := json.Marshal(toFeedbackPageAPI(page))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, required := range []string{
		`"feedback_id"`,
		`"author_display_name"`,
		`"content"`,
		`"disposition"`,
		`"revision"`,
		`"created_at"`,
		`"target"`,
		`"deep_link_target"`,
		`"next_cursor"`,
		`"shot_id"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing allowlisted field %s in %s", required, body)
		}
	}
	lower := strings.ToLower(body)
	for _, forbidden := range []string{
		`"secret"`, `"token"`, `"receipt"`, `"account_id"`, `"price"`, `"cost"`, `"labor"`,
		`"customer_id"`, `"order_id"`, `"ip"`, `"nickname_hash"`, `"raw_token"`,
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("feedback page leaked %q: %s", forbidden, body)
		}
	}
}

func strPtr(v string) *string { return &v }
