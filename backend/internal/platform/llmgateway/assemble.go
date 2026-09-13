package llmgateway

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Options assemble a deployment. Every value is server configuration; no
// caller-supplied field can widen a limit or reach a provider directly.
type Options struct {
	// CatalogPath overrides the built-in directory (CREATIVE_LLM_CATALOG).
	CatalogPath string
	// MonthlyLimitMicros and MonthlyTokenLimit are the server ceilings a
	// photographer's own preference may only lower.
	MonthlyLimitMicros int64
	MonthlyTokenLimit  int64
	Timeout            time.Duration
	Logger             *slog.Logger
	Credential         func(string) (string, bool)
	Clock              func() time.Time
}

const (
	defaultMonthlyLimitMicros = 20_000_000 // 20 USD
	defaultMonthlyTokenLimit  = 5_000_000
	defaultDispatchTimeout    = 120 * time.Second
)

// OptionsFromEnv reads the deployment configuration. Credentials are never read
// here: only the catalog names the environment variable, and the value is
// resolved at dispatch time.
func OptionsFromEnv() (Options, error) {
	opts := Options{
		CatalogPath:        strings.TrimSpace(os.Getenv("CREATIVE_LLM_CATALOG")),
		MonthlyLimitMicros: defaultMonthlyLimitMicros,
		MonthlyTokenLimit:  defaultMonthlyTokenLimit,
		Timeout:            defaultDispatchTimeout,
	}
	for _, field := range []struct {
		env    string
		target *int64
	}{
		{"CREATIVE_LLM_MONTHLY_LIMIT_MICROS", &opts.MonthlyLimitMicros},
		{"CREATIVE_LLM_MONTHLY_TOKEN_LIMIT", &opts.MonthlyTokenLimit},
	} {
		raw := strings.TrimSpace(os.Getenv(field.env))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return Options{}, fmt.Errorf("%w: %s must be a positive integer", ErrValidation, field.env)
		}
		*field.target = value
	}
	return opts, nil
}

// Build assembles the gateway with both wire adapters registered. A deployment
// whose credential is absent stays listed and disabled instead of failing the
// process, so the manual canvas keeps working without any model.
func Build(opts Options) (*Service, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultDispatchTimeout
	}
	if opts.MonthlyLimitMicros <= 0 {
		opts.MonthlyLimitMicros = defaultMonthlyLimitMicros
	}
	if opts.MonthlyTokenLimit <= 0 {
		opts.MonthlyTokenLimit = defaultMonthlyTokenLimit
	}
	credential := opts.Credential
	if credential == nil {
		credential = osCredential
	}

	var (
		catalog *Catalog
		err     error
	)
	if opts.CatalogPath != "" {
		catalog, err = LoadCatalogFile(opts.CatalogPath)
	} else {
		catalog, err = DefaultCatalog(credential)
	}
	if err != nil {
		return nil, err
	}

	client := NewHTTPClient(opts.Timeout)
	return New(Config{
		Catalog: catalog,
		Providers: map[ProviderKey]Provider{
			ProviderOpenAICompatible: NewOpenAICompatible(client),
			ProviderAnthropicMessage: NewAnthropicMessages(client, ""),
		},
		Credential: credential,
		Clock:      opts.Clock,
		Logger:     opts.Logger,
		DefaultBudget: BudgetPolicy{
			MonthlyLimitMicros: opts.MonthlyLimitMicros,
			MonthlyTokenLimit:  opts.MonthlyTokenLimit,
		},
	})
}
