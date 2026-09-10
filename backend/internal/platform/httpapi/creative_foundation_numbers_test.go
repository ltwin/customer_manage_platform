package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCreativeReceiptNumbersSurvivePersistenceAndHTTP(t *testing.T) {
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	if err := scope.Insert(t.Context(), "creative_account_capabilities", []string{"read_enabled", "manual_write_enabled"}, true, true); err != nil {
		t.Fatal(err)
	}
	op := creativeops.Operation{Key: "test_numeric_receipt", Capability: "manual_write", Validate: func(json.RawMessage) error { return nil }, Apply: func(context.Context, store.TxAccountScope, json.RawMessage) (creativeops.Outcome, error) {
		return creativeops.Outcome{HTTPStatus: 200, ResultKind: "test", Response: json.RawMessage(`{"n":9007199254740993,"huge":1e400}`)}, nil
	}}
	cmd := creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{}`)}
	if _, err := (creativeops.Executor{}).Run(t.Context(), scope, op, cmd); err != nil {
		t.Fatal(err)
	}
	response := shootPlanningRequest(t, router, "GET", "/api/v1/creative/operations/"+cmd.OperationID, issueToken(t, tokens, testAcctID), "", "")
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Response map[string]json.Number `json:"response"`
	}
	decoder := json.NewDecoder(strings.NewReader(response.Body.String()))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Response["n"].String() != "9007199254740993" {
		t.Fatalf("integer lost precision: %v", result.Response)
	}
	expected, _ := new(big.Rat).SetString("1e400")
	actual, ok := new(big.Rat).SetString(result.Response["huge"].String())
	if !ok || actual.Cmp(expected) != 0 {
		t.Fatal("large exponent changed")
	}
}
