package planshare_test

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

func TestPlanshareSourcesDoNotImportGinOrLeakAccountScopeOnAnonymousSurface(t *testing.T) {
	root := "."
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read planshare dir: %v", err)
	}
	// Bearer orchestration (application/repository/eligibility) may use AccountScope.
	// Anonymous surface files must stay free of platform/store so HTTP cannot reach
	// AccountScope through token resolve / ports / DTO packages.
	anonymousOnly := map[string]struct{}{
		"token.go": {}, "resolve.go": {}, "ports.go": {}, "dto.go": {},
		"policy.go": {}, "errors.go": {}, "canonical.go": {},
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if path == "github.com/gin-gonic/gin" {
				t.Fatalf("%s imports gin", name)
			}
			if _, guard := anonymousOnly[name]; guard && strings.Contains(path, "/platform/store") {
				t.Fatalf("%s must not import platform/store (anonymous surface)", name)
			}
		}
	}
}

func TestShareTxScopeOmitsAccountScopeMethods(t *testing.T) {
	var iface *planshare.ShareTxScope
	typ := reflect.TypeOf(iface).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if strings.Contains(method.Type.String(), "AccountScope") ||
			strings.Contains(method.Type.String(), "TxAccountScope") {
			t.Fatalf("ShareTxScope.%s exposes account scope: %s", method.Name, method.Type)
		}
	}
}

type memoryLookup struct {
	record planshare.TokenCommitmentRecord
	found  bool
}

func (m memoryLookup) LookupCommitmentBySelector(context.Context, string) (planshare.TokenCommitmentRecord, bool, error) {
	return m.record, m.found, nil
}

func TestResolverReturnsSealedCapabilityWithoutAccountScope(t *testing.T) {
	secret := bytesFilled(32, 7)
	selectorRaw := bytesFilled(12, 3)
	selector, err := planshare.EncodeShareSelector(selectorRaw)
	if err != nil {
		t.Fatalf("selector: %v", err)
	}
	wire, err := planshare.ComposeShareTokenWire(selector, secret)
	if err != nil {
		t.Fatalf("wire: %v", err)
	}
	commitment := planshare.CommitShareSecret(secret)
	resolver := planshare.NewResolver(
		memoryLookup{
			found: true,
			record: planshare.TokenCommitmentRecord{
				Selector:    selector,
				Commitment:  commitment,
				Fingerprint: planshare.ShareFingerprint(selector, commitment),
			},
		},
		planshare.DefaultTrustedCapabilityFactory{},
	)
	ctx, cap, err := resolver.Resolve(context.Background(), wire)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ctx == nil || cap == nil || cap.Context().SelectorFingerprint() == "" {
		t.Fatalf("sealed capability missing fingerprint")
	}
	if _, ok := any(ctx).(interface{ AccountID() string }); ok {
		t.Fatal("ValidatedShareContext must not expose AccountID")
	}
	if _, _, err := resolver.Resolve(context.Background(), wire+"x"); err != planshare.ErrShareNotFound {
		t.Fatalf("invalid token err=%v", err)
	}
	miss := planshare.NewResolver(memoryLookup{found: false}, planshare.DefaultTrustedCapabilityFactory{})
	if _, _, err := miss.Resolve(context.Background(), wire); err != planshare.ErrShareNotFound {
		t.Fatalf("miss err=%v", err)
	}
	_ = txcap.ShareTransactionCapability(cap)
}

func bytesFilled(n int, value byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = value
	}
	return out
}
