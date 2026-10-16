package creativegeneration_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs/versionedfstest"
)

func TestComposeIndependentImageSuppliers(t *testing.T) {
	t.Setenv("CREATIVE_GENERATION_PROTECTION_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	db, err := store.Open(t.Context(), storetest.NewURL(t))
	must(t, err)
	t.Cleanup(db.Close)
	must(t, db.MigrateCreativeJobs(t.Context()))
	cat, err := gw.DefaultCatalog(func(string) (string, bool) { return "", false })
	must(t, err)
	gateway, err := gw.New(gw.Config{Catalog: cat, DefaultBudget: gw.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 1_000_000}})
	must(t, err)
	mc := creativemedia.DefaultConfig()
	media, err := creativemedia.NewService(mc, versionedfstest.NewMemory("local"), creativemedia.NewVerifier(mc), bytes.Repeat([]byte{2}, 32), "/media")
	must(t, err)
	for _, tc := range []struct {
		name   string
		values map[string]string
		models int
		reason string
	}{
		{"unconfigured", nil, 0, "API_KEY"},
		{"existing_only", map[string]string{"AICODEWITH_API_KEY": "a"}, 3, ""},
		{"runninghub_only", map[string]string{"RUNNINGHUB_API_KEY": "r"}, 4, ""},
		{"both", map[string]string{"AICODEWITH_API_KEY": "a", "RUNNINGHUB_API_KEY": "r"}, 6, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := creativegeneration.Compose(gateway, "USD", media, func(k string) (string, bool) { v, ok := tc.values[k]; return v, ok })
			must(t, err)
			if tc.values["RUNNINGHUB_API_KEY"] != "" && c.Service == nil {
				t.Fatal("recovery service disabled with admission")
			}
			if c.Service != nil {
				queue, err := db.NewJobRuntime(c.Service.Handlers(), nil)
				must(t, err)
				c.Service.SetRuntime(queue)
			}
			models := c.Product.Models()
			if len(models) != tc.models {
				t.Fatalf("models=%d want %d", len(models), tc.models)
			}
			if tc.reason != "" && !strings.Contains(c.Disabled, tc.reason) {
				t.Fatalf("reason %q", c.Disabled)
			}
			seen := map[string]bool{}
			for _, m := range models {
				if seen[m.Key] {
					t.Fatalf("duplicated model %s", m.Key)
				}
				seen[m.Key] = true
				if m.Key == "nano-banana-2.1" && m.Label != "Nano Banana 2.1" {
					t.Fatalf("model label %q", m.Label)
				}
				if strings.HasPrefix(m.Key, "nano-banana") {
					for _, p := range m.Parameters {
						if p.Key == "quality" {
							t.Fatal("Nano inherited GPT quality")
						}
					}
				}
			}
			if seen["nano-banana-2.1"] != (tc.values["RUNNINGHUB_API_KEY"] != "") {
				t.Fatal("Nano Banana 2.1 availability must follow RunningHub key")
			}
		})
	}
}
