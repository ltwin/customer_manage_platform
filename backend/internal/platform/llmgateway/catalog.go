package llmgateway

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// A vendor key is shown to the photographer and stored on egress consents, so
// it stays a plain token rather than a path or a URL.
var vendorKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ProviderKey names an adapted wire protocol, not a company. Two deployments of
// the same protocol share one adapter.
type ProviderKey string

const (
	ProviderOpenAICompatible ProviderKey = "openai_compatible"
	ProviderAnthropicMessage ProviderKey = "anthropic_messages"
	ProviderOpenAIResponses  ProviderKey = "openai_responses"
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

// Deployment is one vendor's way of serving a model: where to send, under
// what name and at what price. It is deployment
// configuration, not an editable admin screen, and holds no credential value.
// The photographer never sees it; it exists for dispatch, billing and audit.
type Deployment struct {
	DeploymentKey string      `json:"deployment_key"`
	Provider      ProviderKey `json:"provider"`
	// VendorKey names the company that actually receives the bytes. Every
	// attempt records it, so where content went stays auditable.
	VendorKey     string `json:"vendor_key"`
	BaseURL       string `json:"base_url"`
	CredentialEnv string `json:"credential_env"`
	// RequestModelID is the vendor's own name for the model, which differs
	// between vendors (gpt-6-astra here, gpt-6-astra-0907 there).
	RequestModelID string `json:"request_model_id"`
	// Thinking fixes the deployment's reasoning mode ("", "enabled",
	// "disabled"). It is deployment configuration, never a caller parameter.
	Thinking string `json:"thinking,omitempty"`
	// ReasoningEffort fixes how hard a reasoning model thinks ("low",
	// "medium", "high", "xhigh"): Responses' reasoning effort, or Claude's
	// output_config.effort with adaptive thinking. Empty means the deployment
	// does not reason.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ReasoningContext is an optional relay extension; empty omits it.
	ReasoningContext string        `json:"reasoning_context,omitempty"`
	AcceptedModelIDs []string      `json:"accepted_model_ids"`
	Capability       Capability    `json:"capability"`
	Price            PriceSchedule `json:"price"`
	// Legacy quota fields are accepted only to read existing catalog files.
	// They have no effect on admission and built-in deployments never set them.
	LegacyLimitKey        string `json:"limit_key,omitempty"`
	LegacyConcurrency     int    `json:"concurrency,omitempty"`
	LegacyRateLimitPerMin int    `json:"rate_limit_per_minute,omitempty"`

	// UnacceptedStatuses lists the HTTP statuses this deployment was verified
	// to return *before* processing, i.e. with no charge. Anything not listed
	// stays unknown, because a retry of a possibly billed call is the most
	// expensive mistake this gateway can make.
	UnacceptedStatuses []int  `json:"unaccepted_statuses"`
	QueryCapability    string `json:"query_capability"`
	CancelCapability   string `json:"cancel_capability"`
	Enabled            bool   `json:"enabled"`
	DisabledReason     string `json:"disabled_reason,omitempty"`
}

// Model is what a photographer chooses. Its deployments are listed in priority
// order; every one of them must support the declared capability, so whichever
// serves a request, nothing admitted against the model is beyond it.
type Model struct {
	ModelKey    string     `json:"model_key"`
	DisplayName string     `json:"display_name"`
	Capability  Capability `json:"capability"`
	// ContextWindowTokens is how much input one request may carry. The
	// provider's ContextTokens is what the model can read; this is what the
	// platform is willing to send and pay for every turn. Zero means the hard
	// limit less the output bound.
	ContextWindowTokens int      `json:"context_window_tokens,omitempty"`
	Deployments         []string `json:"deployments"`
}

// ModelConfig is a model as served by one of its deployments: the model's
// identity and declared capability over that deployment's wiring. It is what
// admission, dispatch and the adapters work with.
type ModelConfig struct {
	ModelKey    string
	DisplayName string
	// ContextWindowTokens is the model's configured input window; read it
	// through InputWindowTokens, which supplies the default.
	ContextWindowTokens int
	Deployment
}

// InputWindowTokens is the input one request may carry: the configured window,
// or the hard limit less the output bound when none is configured.
func (m ModelConfig) InputWindowTokens() int {
	if m.ContextWindowTokens > 0 {
		return m.ContextWindowTokens
	}
	return max(1, m.Capability.ContextTokens-m.Capability.MaxOutputTokens)
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
	// ContextWindowTokens is the input window the platform chose to use, which
	// is policy over the provider's hard ContextTokens. Not part of the hash.
	ContextWindowTokens int `json:"context_window_tokens,omitempty"`
	// LimitKey is only present in snapshots frozen before the platform quota
	// was retired; new snapshots leave it empty. Kept so stored rows still read.
	LimitKey         string `json:"limit_key"`
	QueryCapability  string `json:"query_capability"`
	CancelCapability string `json:"cancel_capability"`
	// How the deployment reasoned when the request was prepared. Both are
	// deployment configuration, so a later edit must not rewrite what an old
	// request is remembered to have asked for.
	Thinking        string `json:"thinking,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// CatalogEntry is what a trusted caller may show. It names neither endpoints
// nor vendors: which company serves a model is the platform's choice.
type CatalogEntry struct {
	ModelKey        string     `json:"model_key"`
	DisplayName     string     `json:"display_name"`
	Capability      Capability `json:"capability"`
	CatalogVersion  string     `json:"catalog_version"`
	Available       bool       `json:"available"`
	UnavailableWhy  string     `json:"unavailable_reason,omitempty"`
	ContextTokens   int        `json:"context_tokens"`
	MaxOutputTokens int        `json:"max_output_tokens"`
	// ContextWindowTokens is the effective input window (see
	// ModelConfig.InputWindowTokens), never the provider's hard limit.
	ContextWindowTokens int `json:"context_window_tokens"`
}

// Catalog is the immutable, versioned model directory of one deployment.
type Catalog struct {
	version     string
	currency    string
	models      map[string]Model
	deployments map[string]Deployment
	keys        []string
}

type catalogFile struct {
	Version     string       `json:"version"`
	Currency    string       `json:"currency"`
	Models      []Model      `json:"models"`
	Deployments []Deployment `json:"deployments"`
}

// LoadCatalogFile reads the deployment catalog. Credential values never appear
// in the file; only the environment variable name is configured, and a
// deployment whose credential is absent is disabled with that reason.
func LoadCatalogFile(path string, credential func(string) (string, bool)) (*Catalog, error) {
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
	if credential == nil {
		credential = osCredential
	}
	for i, d := range file.Deployments {
		if _, ok := credential(d.CredentialEnv); d.Enabled && !ok {
			file.Deployments[i].Enabled = false
			file.Deployments[i].DisabledReason = "未配置 " + d.CredentialEnv
		}
	}
	return NewRoutedCatalog(file.Version, file.Currency, file.Models, file.Deployments)
}

// NewCatalog builds a directory where every model has exactly one deployment,
// described together with it.
func NewCatalog(version, currency string, models []ModelConfig) (*Catalog, error) {
	routed := make([]Model, 0, len(models))
	deployments := make([]Deployment, 0, len(models))
	for _, m := range models {
		routed = append(routed, Model{
			ModelKey: m.ModelKey, DisplayName: m.DisplayName,
			Capability: m.Capability, ContextWindowTokens: m.ContextWindowTokens,
			Deployments: []string{m.DeploymentKey},
		})
		deployments = append(deployments, m.Deployment)
	}
	return NewRoutedCatalog(version, currency, routed, deployments)
}

// NewRoutedCatalog validates a directory. A deployment without a usable price
// recipe or output bound cannot be enabled, because no conservative hold could
// be built; a model must be fully supported by every deployment it lists.
func NewRoutedCatalog(version, currency string, models []Model, deployments []Deployment) (*Catalog, error) {
	if version == "" || len(currency) != 3 || strings.ToUpper(currency) != currency {
		return nil, fmt.Errorf("%w: catalog version or currency", ErrValidation)
	}
	c := &Catalog{
		version: version, currency: currency,
		models:      make(map[string]Model, len(models)),
		deployments: make(map[string]Deployment, len(deployments)),
	}
	for _, d := range deployments {
		if err := validateDeployment(d, currency); err != nil {
			return nil, err
		}
		if _, dup := c.deployments[d.DeploymentKey]; dup {
			return nil, fmt.Errorf("%w: duplicate deployment key %q", ErrValidation, d.DeploymentKey)
		}
		c.deployments[d.DeploymentKey] = d
	}
	owner := make(map[string]string, len(deployments))
	for _, m := range models {
		if m.ModelKey == "" || len(m.Deployments) == 0 {
			return nil, fmt.Errorf("%w: model %q needs a key and at least one deployment", ErrValidation, m.ModelKey)
		}
		if _, dup := c.models[m.ModelKey]; dup {
			return nil, fmt.Errorf("%w: duplicate model key %q", ErrValidation, m.ModelKey)
		}
		if m.Capability.ContextTokens <= 0 || m.Capability.MaxOutputTokens <= 0 {
			return nil, fmt.Errorf("%w: model %q capability bounds", ErrValidation, m.ModelKey)
		}
		// The window is input only; it and the output bound must both fit
		// inside what the provider can read.
		if m.ContextWindowTokens < 0 || m.ContextWindowTokens > 0 &&
			m.ContextWindowTokens+m.Capability.MaxOutputTokens > m.Capability.ContextTokens {
			return nil, fmt.Errorf("%w: model %q context window exceeds its capability", ErrValidation, m.ModelKey)
		}
		for _, key := range m.Deployments {
			d, ok := c.deployments[key]
			if !ok {
				return nil, fmt.Errorf("%w: model %q lists unknown deployment %q", ErrValidation, m.ModelKey, key)
			}
			// One deployment, one model: its accepted ids and price answer for
			// exactly one thing the photographer chose.
			if other, taken := owner[key]; taken {
				return nil, fmt.Errorf("%w: deployment %q serves both %q and %q", ErrValidation, key, other, m.ModelKey)
			}
			owner[key] = m.ModelKey
			if !d.Capability.covers(m.Capability) {
				return nil, fmt.Errorf("%w: deployment %q cannot serve everything model %q declares", ErrValidation, key, m.ModelKey)
			}
		}
		c.models[m.ModelKey] = m
		c.keys = append(c.keys, m.ModelKey)
	}
	for key := range c.deployments {
		if _, ok := owner[key]; !ok {
			return nil, fmt.Errorf("%w: deployment %q serves no model", ErrValidation, key)
		}
	}
	sort.Strings(c.keys)
	return c, nil
}

// covers reports whether a deployment really supports everything declared.
func (c Capability) covers(declared Capability) bool {
	need := func(have, want bool) bool { return have || !want }
	return need(c.ImageInput, declared.ImageInput) && need(c.ToolCalling, declared.ToolCalling) &&
		need(c.ToolChoice, declared.ToolChoice) && need(c.JSONObject, declared.JSONObject) &&
		need(c.Temperature, declared.Temperature) &&
		c.ContextTokens >= declared.ContextTokens && c.MaxOutputTokens >= declared.MaxOutputTokens
}

func validateDeployment(m Deployment, currency string) error {
	switch m.Provider {
	case ProviderOpenAICompatible, ProviderAnthropicMessage, ProviderOpenAIResponses:
	default:
		return fmt.Errorf("%w: deployment %q unknown provider", ErrValidation, m.DeploymentKey)
	}
	if m.DeploymentKey == "" || m.RequestModelID == "" {
		return fmt.Errorf("%w: deployment %q identity", ErrValidation, m.DeploymentKey)
	}
	if !vendorKeyPattern.MatchString(m.VendorKey) {
		return fmt.Errorf("%w: deployment %q vendor key", ErrValidation, m.DeploymentKey)
	}
	if !strings.HasPrefix(m.BaseURL, "https://") {
		return fmt.Errorf("%w: deployment %q base url must be https", ErrValidation, m.DeploymentKey)
	}
	if m.CredentialEnv == "" || strings.ContainsAny(m.CredentialEnv, " \t\n") {
		return fmt.Errorf("%w: deployment %q credential env reference", ErrValidation, m.DeploymentKey)
	}
	if len(m.AcceptedModelIDs) == 0 {
		return fmt.Errorf("%w: deployment %q needs accepted model ids", ErrValidation, m.DeploymentKey)
	}
	if m.Capability.ContextTokens <= 0 || m.Capability.MaxOutputTokens <= 0 {
		return fmt.Errorf("%w: deployment %q capability bounds", ErrValidation, m.DeploymentKey)
	}
	switch m.Thinking {
	case "", "enabled", "disabled":
	default:
		return fmt.Errorf("%w: deployment %q thinking mode", ErrValidation, m.DeploymentKey)
	}
	switch {
	case m.ReasoningEffort == "":
	case m.Provider != ProviderOpenAIResponses && m.Provider != ProviderAnthropicMessage:
		return fmt.Errorf("%w: deployment %q reasoning effort needs the responses or anthropic protocol", ErrValidation, m.DeploymentKey)
	case !slices.Contains([]string{"low", "medium", "high", "xhigh"}, m.ReasoningEffort):
		return fmt.Errorf("%w: deployment %q reasoning effort", ErrValidation, m.DeploymentKey)
	}
	if m.ReasoningContext != "" && (m.ReasoningContext != "all_turns" || m.Provider != ProviderOpenAIResponses || m.ReasoningEffort == "") {
		return fmt.Errorf("%w: deployment %q reasoning context", ErrValidation, m.DeploymentKey)
	}
	switch m.QueryCapability {
	case "none", "provider_query":
	default:
		return fmt.Errorf("%w: deployment %q query capability", ErrValidation, m.DeploymentKey)
	}
	switch m.CancelCapability {
	case "none", "provider_cancel":
	default:
		return fmt.Errorf("%w: deployment %q cancel capability", ErrValidation, m.DeploymentKey)
	}
	if m.Price.Version == "" || m.Price.Currency != currency {
		return fmt.Errorf("%w: deployment %q price version or currency", ErrValidation, m.DeploymentKey)
	}
	bound := m.Price.UpperBound()
	if bound.InputUncached <= 0 || bound.Output <= 0 || bound.InputCached < 0 {
		return fmt.Errorf("%w: deployment %q has no usable price recipe", ErrValidation, m.DeploymentKey)
	}
	return nil
}

// Version reports the catalog generation persisted on every request.
func (c *Catalog) Version() string { return c.version }

// Currency reports the single configured settlement currency.
func (c *Catalog) Currency() string { return c.currency }

// Vendors lists every company this deployment can send to, including vendors
// whose models are currently disabled: an existing consent stays meaningful
// while a model is switched off.
func (c *Catalog) Vendors() []string {
	seen := make(map[string]bool, len(c.deployments))
	vendors := make([]string, 0, len(c.deployments))
	for _, d := range c.deployments {
		if !seen[d.VendorKey] {
			seen[d.VendorKey] = true
			vendors = append(vendors, d.VendorKey)
		}
	}
	sort.Strings(vendors)
	return vendors
}

// List returns the selectable models. Disabled entries keep a visible reason
// instead of silently disappearing.
func (c *Catalog) List() []CatalogEntry {
	entries := make([]CatalogEntry, 0, len(c.keys))
	for _, key := range c.keys {
		m := c.models[key]
		_, err := c.route(m)
		entry := CatalogEntry{
			ModelKey:        m.ModelKey,
			DisplayName:     m.DisplayName,
			Capability:      m.Capability,
			CatalogVersion:  c.version,
			Available:       err == nil,
			ContextTokens:   m.Capability.ContextTokens,
			MaxOutputTokens: m.Capability.MaxOutputTokens,
			ContextWindowTokens: ModelConfig{ContextWindowTokens: m.ContextWindowTokens,
				Deployment: Deployment{Capability: m.Capability}}.InputWindowTokens(),
		}
		if err != nil {
			entry.UnavailableWhy = c.deployments[m.Deployments[0]].DisabledReason
		}
		entries = append(entries, entry)
	}
	return entries
}

// Model resolves a model to the deployment that serves it right now.
func (c *Catalog) Model(key string) (ModelConfig, error) {
	m, ok := c.models[key]
	if !ok {
		return ModelConfig{}, fmt.Errorf("%w: model %q", ErrNotFound, key)
	}
	d, err := c.route(m)
	if err != nil {
		return ModelConfig{}, err
	}
	return serve(m, d), nil
}

// Deployment resolves a model through the deployment a request was frozen
// against, so a later change of priority never moves a prepared call.
func (c *Catalog) Deployment(modelKey, deploymentKey string) (ModelConfig, error) {
	m, ok := c.models[modelKey]
	if !ok {
		return ModelConfig{}, fmt.Errorf("%w: model %q", ErrNotFound, modelKey)
	}
	if !slices.Contains(m.Deployments, deploymentKey) {
		// What was frozen is no longer offered: a conflict with the live
		// directory, never a reason to send somewhere else.
		return ModelConfig{}, fmt.Errorf("%w: model %q no longer has deployment %q", ErrConflict, modelKey, deploymentKey)
	}
	d := c.deployments[deploymentKey]
	if !d.Enabled {
		return ModelConfig{}, fmt.Errorf("%w: deployment %q: %s", ErrUnavailable, deploymentKey, d.DisabledReason)
	}
	return serve(m, d), nil
}

// route picks the deployment that serves a model: the first enabled one in
// priority order. This is the one place an administrator's ordering will plug
// in later; everything downstream only sees its answer.
func (c *Catalog) route(m Model) (Deployment, error) {
	for _, key := range m.Deployments {
		if d := c.deployments[key]; d.Enabled {
			return d, nil
		}
	}
	return Deployment{}, fmt.Errorf("%w: model %q: %s", ErrUnavailable, m.ModelKey, c.deployments[m.Deployments[0]].DisabledReason)
}

// enabledDeployments lists what may be dispatched to, for wiring checks.
func (c *Catalog) enabledDeployments() []Deployment {
	var out []Deployment
	for _, d := range c.deployments {
		if d.Enabled {
			out = append(out, d)
		}
	}
	return out
}

// serve presents a deployment under the model it serves. The declared
// capability is the model's: that is what requests are admitted against.
func serve(m Model, d Deployment) ModelConfig {
	d.Capability = m.Capability
	return ModelConfig{ModelKey: m.ModelKey, DisplayName: m.DisplayName, ContextWindowTokens: m.ContextWindowTokens, Deployment: d}
}

// Snapshot freezes the credential-free identity persisted with a request.
func (c *Catalog) Snapshot(m ModelConfig) ModelSnapshot {
	accepted := append([]string(nil), m.AcceptedModelIDs...)
	sort.Strings(accepted)
	return ModelSnapshot{
		ModelKey:            m.ModelKey,
		CatalogVersion:      c.version,
		DeploymentKey:       m.DeploymentKey,
		Provider:            m.Provider,
		RequestModelID:      m.RequestModelID,
		AcceptedModelIDs:    accepted,
		PriceVersion:        m.Price.Version,
		Currency:            m.Price.Currency,
		MaxOutputTokens:     m.Capability.MaxOutputTokens,
		ContextTokens:       m.Capability.ContextTokens,
		ContextWindowTokens: m.InputWindowTokens(),
		QueryCapability:     m.QueryCapability,
		CancelCapability:    m.CancelCapability,
		Thinking:            m.Thinking,
		ReasoningEffort:     m.ReasoningEffort,
	}
}

// HandsBackReasoning reports whether earlier turns' reasoning must travel back
// to this deployment, so history keeps it instead of dropping it.
func (m Deployment) HandsBackReasoning() bool {
	return m.Thinking == "enabled" || m.ReasoningEffort != ""
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
