package httpapi

import (
	"reflect"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

func TestAnonymousShareRouteDepsOmitAccountScope(t *testing.T) {
	typ := reflect.TypeOf(anonymousShareRouteDeps{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := field.Type.String()
		if strings.Contains(name, "AccountScope") ||
			strings.Contains(name, "ScopeFactory") ||
			strings.Contains(name, "TxAccountScope") {
			t.Fatalf("anonymous share deps field %s exposes %s", field.Name, name)
		}
	}
	_ = anonymousShareRouteDeps{
		Resolver: planshare.ShareTokenResolver(nil),
		Runner:   txcap.TransactionRunner[planshare.ShareTxScope](nil),
	}
}
