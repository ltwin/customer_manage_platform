package llmgateway

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"
)

// ProviderKey names an adapted wire protocol, not a company. Two deployments of
// the same protocol share one adapter.
type ProviderKey string

const (
	ProviderOpenAICompatible ProviderKey = "openai_compatible"
	ProviderAnthropicMessage ProviderKey = "anthropic_messages"
)

// Capability is what the deployment really supports. A request that needs more
// is refused before dispatch; fields are never silently dropped.
type Capability struct {
	ImageInput  bool `json:"image_input"`
	ToolCalling bool `json:"tool_calling"`
	// ToolChoice reports whether the deployment honours an explicit
	// tool_choice. DeepSeek's thinking mode, for example, refuses it.
	ToolChoice      bool `json:"tool_choice"`
	JSONObject      bool `json:"json_object"`
	Temperature     bool `json:"temperature"`
	ContextTokens   int  `json:"context_tokens"`
	MaxOutputTokens int  `json:"max_output_tokens"`
}

// Rate is micros of the configured currency per one million tokens.
type Rate struct {
	InputCached   int64 `json:"input_cached"`
	InputUncached int64 `json:"input_uncached"`
	Output        int64 `json:"output"`
}

// PeakWindow is a UTC weekday range where the peak rate applies.
type PeakWindow struct {
	Weekdays  []int `json:"weekdays"`
	StartHour int   `json:"start_hour"`
	EndHour   int   `json:"end_hour"`
}

func (w PeakWindow) contains(t time.Time) bool {
	t = t.UTC()
	hour := t.Hour()
	if hour < w.StartHour || hour >= w.EndHour {
		return false
	}
	for _, d := range w.Weekdays {
		if int(t.Weekday()) == d {
			return true
		}
	}
	return false
}

// PriceSchedule is a fixed, versioned recipe of non-overlapping components.
// Included dimensions are converted to disjoint parts: uncached input is
// prompt tokens minus cached tokens, and reasoning stays inside output.
type PriceSchedule struct {
	Version     string       `json:"version"`
	Currency    string       `json:"currency"`
	Source      string       `json:"source"`
	Peak        Rate         `json:"peak"`
	OffPeak     Rate         `json:"off_peak"`
	PeakWindows []PeakWindow `json:"peak_windows"`
}

// RateAt returns the rate that applies to a dispatch timestamp.
func (p PriceSchedule) RateAt(t time.Time) Rate {
	for _, w := range p.PeakWindows {
		if w.contains(t) {
			return p.Peak
		}
	}
	return p.OffPeak
}

// UpperBound is the conservative rate used for reservations; it never assumes
// the cheaper window will still apply when the request is finally dispatched.
func (p PriceSchedule) UpperBound() Rate {
	return Rate{
		InputCached:   max64(p.Peak.InputCached, p.OffPeak.InputCached),
		InputUncached: max64(p.Peak.InputUncached, p.OffPeak.InputUncached),
		Output:        max64(p.Peak.Output, p.OffPeak.Output),
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// CostMicros converts tokens at one rate using exact rational arithmetic and
// rounds up, so a fractional micro is never billed as zero.
func CostMicros(tokens int64, ratePerMillion int64) int64 {
	if tokens <= 0 || ratePerMillion <= 0 {
		return 0
	}
	product := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(ratePerMillion))
	quotient, remainder := new(big.Int).QuoRem(product, big.NewInt(1_000_000), new(big.Int))
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient.Int64()
}

// ModelConfig is one deployment entry of the versioned catalog. It is deployment
// configuration, not an editable admin screen, and holds no credential value.
type ModelConfig struct {
	ModelKey       string      `json:"model_key"`
	DisplayName    string      `json:"display_name"`
	Provider       ProviderKey `json:"provider"`
	DeploymentKey  string      `json:"deployment_key"`
	BaseURL        string      `json:"base_url"`
	CredentialEnv  string      `json:"credential_env"`
	RequestModelID string      `json:"request_model_id"`
	// Thinking fixes the deployment's reasoning mode ("", "enabled",
	// "disabled"). It is deployment configuration, never a caller parameter.
	Thinking         string        `json:"thinking"`
	AcceptedModelIDs []string      `json:"accepted_model_ids"`
	Capability       Capability    `json:"capability"`
	Price            PriceSchedule `json:"price"`
	LimitKey         string        `json:"limit_key"`
	Concurrency      int           `json:"concurrency"`
	RateLimitPerMin  int           `json:"rate_limit_per_minute"`
	// UnacceptedStatuses lists the HTTP statuses this deployment was verified
	// to return *before* processing, i.e. with no charge. Anything not listed
	// stays unknown, because a retry of a possibly billed call is the most
	// expensive mistake this gateway can make.
	UnacceptedStatuses []int  `json:"unaccepted_statuses"`
	QueryCapability    string `json:"query_capability"`
	CancelCapability   string `json:"cancel_capability"`
	Enabled            bool   `json:"enabled"`
	DisabledReason     string `json:"disabled_reason"`
}

// ModelSnapshot is the credential-free projection stored with every request.
// A later catalog edit never changes an in-flight request.
type ModelSnapshot struct {
	ModelKey         string      `json:"model_key"`
	CatalogVersion   string      `json:"catalog_version"`
	DeploymentKey    string      `json:"deployment_key"`
	Provider         ProviderKey `json:"provider"`
	RequestModelID   string      `json:"request_model_id"`
	AcceptedModelIDs []string    `json:"accepted_model_ids"`
	PriceVersion     string      `json:"price_version"`
	Currency         string      `json:"currency"`
	MaxOutputTokens  int         `json:"max_output_tokens"`
	ContextTokens    int         `json:"context_tokens"`
	LimitKey         string      `json:"limit_key"`
	QueryCapability  string      `json:"query_capability"`
	CancelCapability string      `json:"cancel_capability"`
}

// CatalogEntry is what a trusted caller may show; it never leaks endpoints.
type CatalogEntry struct {
	ModelKey        string     `json:"model_key"`
	DisplayName     string     `json:"display_name"`
	Provider        string     `json:"provider"`
	Capability      Capability `json:"capability"`
	CatalogVersion  string     `json:"catalog_version"`
	Available       bool       `json:"available"`
	UnavailableWhy  string     `json:"unavailable_reason,omitempty"`
	ContextTokens   int        `json:"context_tokens"`
	MaxOutputTokens int        `json:"max_output_tokens"`
}

// Catalog is the immutable, versioned model directory of one deployment.
type Catalog struct {
	version  string
	currency string
	models   map[string]ModelConfig
	keys     []string
}

type catalogFile struct {
	Version  string        `json:"version"`
	Currency string        `json:"currency"`
	Models   []ModelConfig `json:"models"`
}

// LoadCatalogFile reads the deployment catalog. Credential values never appear
// in the file; only the environment variable name is configured.
func LoadCatalogFile(path string) (*Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model catalog: %w", err)
	}
	var file catalogFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("%w: model catalog: %w", ErrValidation, err)
	}
	return NewCatalog(file.Version, file.Currency, file.Models)
}

// NewCatalog validates a directory. A model without a usable price recipe or
// output bound cannot be enabled, because no conservative hold could be built.
func NewCatalog(version, currency string, models []ModelConfig) (*Catalog, error) {
	if version == "" || len(currency) != 3 || strings.ToUpper(currency) != currency {
		return nil, fmt.Errorf("%w: catalog version or currency", ErrValidation)
	}
	c := &Catalog{version: version, currency: currency, models: make(map[string]ModelConfig, len(models))}
	for _, m := range models {
		if err := validateModel(m, currency); err != nil {
			return nil, err
		}
		if _, dup := c.models[m.ModelKey]; dup {
			return nil, fmt.Errorf("%w: duplicate model key %q", ErrValidation, m.ModelKey)
		}
		c.models[m.ModelKey] = m
		c.keys = append(c.keys, m.ModelKey)
	}
	sort.Strings(c.keys)
	// Deployments that share one credential share one admission record, so they
	// must agree on its shape; otherwise every dispatch would rewrite the
	// capacity of the previous one.
	shared := make(map[string]ModelConfig, len(models))
	for _, key := range c.keys {
		m := c.models[key]
		first, seen := shared[m.LimitKey]
		if !seen {
			shared[m.LimitKey] = m
			continue
		}
		if first.Concurrency != m.Concurrency || first.RateLimitPerMin != m.RateLimitPerMin {
			return nil, fmt.Errorf("%w: models %q and %q share limit key %q with different limits",
				ErrValidation, first.ModelKey, m.ModelKey, m.LimitKey)
		}
	}
	return c, nil
}

func validateModel(m ModelConfig, currency string) error {
	switch m.Provider {
	case ProviderOpenAICompatible, ProviderAnthropicMessage:
	default:
		return fmt.Errorf("%w: model %q unknown provider", ErrValidation, m.ModelKey)
	}
	if m.ModelKey == "" || m.DeploymentKey == "" || m.RequestModelID == "" || m.LimitKey == "" {
		return fmt.Errorf("%w: model %q identity", ErrValidation, m.ModelKey)
	}
	if !strings.HasPrefix(m.BaseURL, "https://") {
		return fmt.Errorf("%w: model %q base url must be https", ErrValidation, m.ModelKey)
	}
	if m.CredentialEnv == "" || strings.ContainsAny(m.CredentialEnv, " \t\n") {
		return fmt.Errorf("%w: model %q credential env reference", ErrValidation, m.ModelKey)
	}
	if len(m.AcceptedModelIDs) == 0 {
		return fmt.Errorf("%w: model %q needs accepted model ids", ErrValidation, m.ModelKey)
	}
	if m.Capability.ContextTokens <= 0 || m.Capability.MaxOutputTokens <= 0 {
		return fmt.Errorf("%w: model %q capability bounds", ErrValidation, m.ModelKey)
	}
	if m.Concurrency <= 0 || m.RateLimitPerMin <= 0 {
		return fmt.Errorf("%w: model %q admission limits", ErrValidation, m.ModelKey)
	}
	switch m.Thinking {
	case "", "enabled", "disabled":
	default:
		return fmt.Errorf("%w: model %q thinking mode", ErrValidation, m.ModelKey)
	}
	switch m.QueryCapability {
	case "none", "provider_query":
	default:
		return fmt.Errorf("%w: model %q query capability", ErrValidation, m.ModelKey)
	}
	switch m.CancelCapability {
	case "none", "provider_cancel":
	default:
		return fmt.Errorf("%w: model %q cancel capability", ErrValidation, m.ModelKey)
	}
	if m.Price.Version == "" || m.Price.Currency != currency {
		return fmt.Errorf("%w: model %q price version or currency", ErrValidation, m.ModelKey)
	}
	bound := m.Price.UpperBound()
	if bound.InputUncached <= 0 || bound.Output <= 0 || bound.InputCached < 0 {
		return fmt.Errorf("%w: model %q has no usable price recipe", ErrValidation, m.ModelKey)
	}
	return nil
}

// Version reports the catalog generation persisted on every request.
func (c *Catalog) Version() string { return c.version }

// Currency reports the single configured settlement currency.
func (c *Catalog) Currency() string { return c.currency }

// List returns the selectable models. Disabled entries keep a visible reason
// instead of silently disappearing.
func (c *Catalog) List() []CatalogEntry {
	entries := make([]CatalogEntry, 0, len(c.keys))
	for _, key := range c.keys {
		m := c.models[key]
		entries = append(entries, CatalogEntry{
			ModelKey:        m.ModelKey,
			DisplayName:     m.DisplayName,
			Provider:        string(m.Provider),
			Capability:      m.Capability,
			CatalogVersion:  c.version,
			Available:       m.Enabled,
			UnavailableWhy:  m.DisabledReason,
			ContextTokens:   m.Capability.ContextTokens,
			MaxOutputTokens: m.Capability.MaxOutputTokens,
		})
	}
	return entries
}

// Model resolves an enabled deployment for dispatch.
func (c *Catalog) Model(key string) (ModelConfig, error) {
	m, ok := c.models[key]
	if !ok {
		return ModelConfig{}, fmt.Errorf("%w: model %q", ErrNotFound, key)
	}
	if !m.Enabled {
		return ModelConfig{}, fmt.Errorf("%w: model %q: %s", ErrUnavailable, key, m.DisabledReason)
	}
	return m, nil
}

// Snapshot freezes the credential-free identity persisted with a request.
func (c *Catalog) Snapshot(m ModelConfig) ModelSnapshot {
	accepted := append([]string(nil), m.AcceptedModelIDs...)
	sort.Strings(accepted)
	return ModelSnapshot{
		ModelKey:         m.ModelKey,
		CatalogVersion:   c.version,
		DeploymentKey:    m.DeploymentKey,
		Provider:         m.Provider,
		RequestModelID:   m.RequestModelID,
		AcceptedModelIDs: accepted,
		PriceVersion:     m.Price.Version,
		Currency:         m.Price.Currency,
		MaxOutputTokens:  m.Capability.MaxOutputTokens,
		ContextTokens:    m.Capability.ContextTokens,
		LimitKey:         m.LimitKey,
		QueryCapability:  m.QueryCapability,
		CancelCapability: m.CancelCapability,
	}
}

// CheckCapability refuses a request the deployment cannot really serve.
func CheckCapability(r ChatRequest, m ModelConfig) error {
	if r.OutputLimit > m.Capability.MaxOutputTokens {
		return fmt.Errorf("%w: output limit above %d", ErrCapability, m.Capability.MaxOutputTokens)
	}
	if len(r.Tools) > 0 && !m.Capability.ToolCalling {
		return fmt.Errorf("%w: tools", ErrCapability)
	}
	if r.ToolChoice != "" && !m.Capability.ToolChoice {
		return fmt.Errorf("%w: explicit tool choice", ErrCapability)
	}
	if r.ResponseFormat == "json_object" && !m.Capability.JSONObject {
		return fmt.Errorf("%w: json output", ErrCapability)
	}
	if r.Temperature != nil && !m.Capability.Temperature {
		return fmt.Errorf("%w: temperature", ErrCapability)
	}
	for _, msg := range r.Messages {
		for _, b := range msg.Blocks {
			if b.Kind == BlockImageInput && !m.Capability.ImageInput {
				return fmt.Errorf("%w: image input", ErrCapability)
			}
		}
	}
	return nil
}

// AcceptsModelID reports whether a provider-returned model name matches the
// fixed deployment. An unexpected routing change is a protocol error.
func (s ModelSnapshot) AcceptsModelID(id string) bool {
	for _, accepted := range s.AcceptedModelIDs {
		if accepted == id {
			return true
		}
	}
	return false
}
