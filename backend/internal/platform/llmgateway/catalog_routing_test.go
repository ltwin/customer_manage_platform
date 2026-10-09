package llmgateway_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func routedDeployment(key, vendor string) llmgateway.Deployment {
	return llmgateway.Deployment{
		Provider: llmgateway.ProviderOpenAICompatible, VendorKey: vendor, DeploymentKey: key,
		BaseURL: "https://example.invalid", CredentialEnv: "TEST_KEY", RequestModelID: "vendor-model",
		AcceptedModelIDs: []string{"vendor-model"},
		Capability:       llmgateway.Capability{ImageInput: true, ToolCalling: true, ContextTokens: 1000, MaxOutputTokens: 500},
		Price:            usablePrice(), LimitKey: key, Concurrency: 2, RateLimitPerMin: 10,
		QueryCapability: "none", CancelCapability: "none", Enabled: true,
	}
}

func routedModel(deployments ...string) llmgateway.Model {
	return llmgateway.Model{
		ModelKey: "chat", DisplayName: "Chat", Deployments: deployments,
		Capability: llmgateway.Capability{ImageInput: true, ToolCalling: true, ContextTokens: 1000, MaxOutputTokens: 500},
	}
}

func TestRoutedCatalogRefusesWhatItCannotServe(t *testing.T) {
	blind := routedDeployment("blind", "b")
	blind.Capability.ImageInput = false
	cases := map[string]struct {
		models      []llmgateway.Model
		deployments []llmgateway.Deployment
	}{
		// Failing over to it would send an image to a model that cannot see.
		"weaker deployment":  {[]llmgateway.Model{routedModel("a", "blind")}, []llmgateway.Deployment{routedDeployment("a", "a"), blind}},
		"unknown deployment": {[]llmgateway.Model{routedModel("a", "missing")}, []llmgateway.Deployment{routedDeployment("a", "a")}},
		"shared deployment": {
			[]llmgateway.Model{routedModel("a"), {ModelKey: "other", Capability: routedModel().Capability, Deployments: []string{"a"}}},
			[]llmgateway.Deployment{routedDeployment("a", "a")},
		},
		"orphan deployment": {[]llmgateway.Model{routedModel("a")}, []llmgateway.Deployment{routedDeployment("a", "a"), routedDeployment("b", "b")}},
		"no deployment":     {[]llmgateway.Model{routedModel()}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := llmgateway.NewRoutedCatalog("cat-1", "USD", c.models, c.deployments); !errors.Is(err, llmgateway.ErrValidation) {
				t.Fatalf("expected a validation error, got %v", err)
			}
		})
	}
}

func TestAModelIsServedByItsFirstEnabledDeployment(t *testing.T) {
	primary, backup := routedDeployment("primary", "first-vendor"), routedDeployment("backup", "second-vendor")
	primary.Enabled, primary.DisabledReason = false, "未配置 PRIMARY_KEY"
	// The backup does more than the model promises; requests are still held to
	// the model's declaration.
	backup.Capability.Temperature = true
	catalog, err := llmgateway.NewRoutedCatalog("cat-1", "USD", []llmgateway.Model{routedModel("primary", "backup")}, []llmgateway.Deployment{primary, backup})
	if err != nil {
		t.Fatal(err)
	}
	served, err := catalog.Model("chat")
	if err != nil {
		t.Fatal(err)
	}
	if served.DeploymentKey != "backup" || served.ModelKey != "chat" || served.Capability.Temperature {
		t.Fatalf("served %+v", served)
	}
	if _, err := catalog.Deployment("chat", "primary"); !errors.Is(err, llmgateway.ErrUnavailable) {
		t.Fatalf("a disabled deployment is not dispatchable: %v", err)
	}
	entries := catalog.List()
	if len(entries) != 1 || !entries[0].Available {
		t.Fatalf("entries %+v", entries)
	}
	// The photographer sees the model, never who serves it.
	raw, _ := json.Marshal(entries)
	if strings.Contains(string(raw), "vendor") || strings.Contains(string(raw), "provider") {
		t.Fatalf("catalog entry names a vendor: %s", raw)
	}

	backup.Enabled, backup.DisabledReason = false, "未配置 BACKUP_KEY"
	catalog, err = llmgateway.NewRoutedCatalog("cat-1", "USD", []llmgateway.Model{routedModel("primary", "backup")}, []llmgateway.Deployment{primary, backup})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Model("chat"); !errors.Is(err, llmgateway.ErrUnavailable) {
		t.Fatalf("no deployment left: %v", err)
	}
	if entry := catalog.List()[0]; entry.Available || entry.UnavailableWhy != "未配置 PRIMARY_KEY" {
		t.Fatalf("entry %+v", entry)
	}
}

func TestACatalogFileRoutesAroundAMissingCredential(t *testing.T) {
	primary, backup := routedDeployment("primary", "first-vendor"), routedDeployment("backup", "second-vendor")
	primary.CredentialEnv, backup.CredentialEnv = "PRIMARY_KEY", "BACKUP_KEY"
	raw, err := json.Marshal(map[string]any{
		"version": "file-1", "currency": "USD",
		"models":      []llmgateway.Model{routedModel("primary", "backup")},
		"deployments": []llmgateway.Deployment{primary, backup},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := llmgateway.LoadCatalogFile(path, func(env string) (string, bool) { return "k", env == "BACKUP_KEY" })
	if err != nil {
		t.Fatal(err)
	}
	served, err := catalog.Model("chat")
	if err != nil || served.DeploymentKey != "backup" {
		t.Fatalf("served %+v err %v", served, err)
	}
}

// A prepared call goes where it was frozen even after the order changes; the
// next one follows the new order.
func TestReorderingNeverMovesAPreparedCall(t *testing.T) {
	ordered := func(keys ...string) *llmgateway.Catalog {
		t.Helper()
		catalog, err := llmgateway.NewRoutedCatalog("cat-1", "USD", []llmgateway.Model{routedModel(keys...)},
			[]llmgateway.Deployment{routedDeployment("primary", "first-vendor"), routedDeployment("backup", "second-vendor")})
		if err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	f := setupGatewayWith(t, defaultBudget(), ordered("primary", "backup"))
	var sentTo []string
	f.provider.observe = func(r llmgateway.ProviderRequest) { sentTo = append(sentTo, r.Model.DeploymentKey) }
	reservation := f.reserve(t, f.a, uuid.NewString(), turnOptions{})
	view, chat, err := f.prepare(t, f.a, reservation.ID, uuid.NewString(), turnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := f.begin(t, f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}

	reordered, err := llmgateway.New(llmgateway.Config{
		Catalog:       ordered("backup", "primary"),
		Providers:     map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.provider},
		Credential:    func(string) (string, bool) { return "secret-value", true },
		Clock:         func() time.Time { return f.now },
		DefaultBudget: defaultBudget(),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.gateway = reordered
	if _, err := reordered.Execute(t.Context(), f.a, limitsOf, permit, chat, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.turn(t, f.a, turnOptions{group: "run-2"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(sentTo, ",") != "primary,backup" {
		t.Fatalf("sent to %v", sentTo)
	}
}
