package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

type availabilityWindowResponse struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type settingsAvailabilityResponse struct {
	Timezone     string `json:"timezone"`
	Availability struct {
		Weekly            map[string]*availabilityWindowResponse `json:"weekly"`
		MinOpeningMinutes int                                    `json:"min_opening_minutes"`
		TurnaroundMinutes int                                    `json:"turnaround_minutes"`
	} `json:"availability"`
}

func TestSettingsAvailabilityDefaultsValidationIsolationAndCorruption(t *testing.T) {
	router, database, issuer := newCustomerAPIRouter(t)
	tokenA := issueToken(t, issuer, testAcctID)
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash second account password: %v", err)
	}
	const accountB = "acct-settings-b"
	if err := database.CreateAccount(context.Background(), accountB, hash); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	tokenB := issueToken(t, issuer, accountB)

	defaults := getSettingsAvailability(t, router, tokenA)
	if defaults.Availability.MinOpeningMinutes != 120 || defaults.Availability.TurnaroundMinutes != 60 ||
		len(defaults.Availability.Weekly) != 7 || defaults.Availability.Weekly["1"].Start != "10:00" ||
		defaults.Availability.Weekly["7"].End != "20:00" {
		t.Fatalf("default availability is incomplete: %+v", defaults.Availability)
	}

	valid := []byte(`{
		"availability": {
			"weekly": {
				"1":{"start":"08:30","end":"17:30"},
				"2":null,
				"3":{"start":"10:00","end":"19:00"},
				"4":{"start":"10:00","end":"19:00"},
				"5":{"start":"10:00","end":"19:00"},
				"6":{"start":"09:00","end":"20:00"},
				"7":null
			},
			"min_opening_minutes":90,
			"turnaround_minutes":30
		}
	}`)
	rec := authenticatedRequest(t, router, http.MethodPatch, "/api/v1/settings", tokenA, valid)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid availability patch: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	updated := getSettingsAvailability(t, router, tokenA)
	if updated.Availability.Weekly["1"].Start != "08:30" || updated.Availability.Weekly["2"] != nil ||
		updated.Availability.MinOpeningMinutes != 90 || updated.Availability.TurnaroundMinutes != 30 {
		t.Fatalf("updated availability = %+v", updated.Availability)
	}
	foreign := getSettingsAvailability(t, router, tokenB)
	if foreign.Availability.Weekly["1"].Start != "10:00" || foreign.Availability.MinOpeningMinutes != 120 {
		t.Fatalf("account B observed account A settings: %+v", foreign.Availability)
	}

	invalidBodies := map[string]string{
		"missing weekday": `{"availability":{"weekly":{"1":null,"2":null,"3":null,"4":null,"5":null,"6":null},"min_opening_minutes":120,"turnaround_minutes":60}}`,
		"extra weekday":   `{"availability":{"weekly":{"1":null,"2":null,"3":null,"4":null,"5":null,"6":null,"7":null,"8":null},"min_opening_minutes":120,"turnaround_minutes":60}}`,
		"bad time":        `{"availability":{"weekly":{"1":{"start":"8:00","end":"19:00"},"2":null,"3":null,"4":null,"5":null,"6":null,"7":null},"min_opening_minutes":120,"turnaround_minutes":60}}`,
		"reversed window": `{"availability":{"weekly":{"1":{"start":"19:00","end":"10:00"},"2":null,"3":null,"4":null,"5":null,"6":null,"7":null},"min_opening_minutes":120,"turnaround_minutes":60}}`,
		"short minimum":   `{"availability":{"weekly":{"1":null,"2":null,"3":null,"4":null,"5":null,"6":null,"7":null},"min_opening_minutes":14,"turnaround_minutes":60}}`,
		"long turnaround": `{"availability":{"weekly":{"1":null,"2":null,"3":null,"4":null,"5":null,"6":null,"7":null},"min_opening_minutes":120,"turnaround_minutes":241}}`,
	}
	for name, body := range invalidBodies {
		t.Run(name, func(t *testing.T) {
			rec := authenticatedRequest(t, router, http.MethodPatch, "/api/v1/settings", tokenA, []byte(body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid patch: want 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	unchanged := getSettingsAvailability(t, router, tokenA)
	if unchanged.Availability.Weekly["1"].Start != "08:30" || unchanged.Availability.MinOpeningMinutes != 90 {
		t.Fatalf("invalid patch changed stored value: %+v", unchanged.Availability)
	}

	scopeA := database.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	if _, err := scopeA.Update(context.Background(), "settings", "availability = $2::jsonb", "",
		`{"weekly":{"1":null},"min_opening_minutes":120,"turnaround_minutes":60}`); err != nil {
		t.Fatalf("corrupt stored availability fixture: %v", err)
	}
	rec = authenticatedRequest(t, router, http.MethodGet, "/api/v1/settings", tokenA, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt stored availability: want 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func getSettingsAvailability(t *testing.T, router http.Handler, token string) settingsAvailabilityResponse {
	t.Helper()
	rec := authenticatedRequest(t, router, http.MethodGet, "/api/v1/settings", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var response settingsAvailabilityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return response
}
