package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

const (
	// Scope is the only scope there is: one grant covers every creative tool.
	Scope = "creative"
	// ActivityWindow is how long after its last call a connection counts as
	// connected in the workspace.
	ActivityWindow = 5 * time.Minute

	accessTokenTTL   = time.Hour
	codeTTL          = 60 * time.Second
	requestTTL       = 10 * time.Minute
	refreshIdle      = 30 * 24 * time.Hour
	refreshAbsolute  = 90 * 24 * time.Hour
	touchInterval    = time.Minute
	registrationRate = 20 // per IP per hour

	accessKeyLabel  = "crm-auth/v1/mcp-access"
	requestKeyLabel = "crm-auth/v1/mcp-authorize-request"
)

// Error is an OAuth protocol error: the code goes on the wire as `error`.
type Error struct {
	Code        string
	Description string
	// RetryAfter is set on registration throttling.
	RetryAfter time.Duration
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func oauthError(code, description string) *Error { return &Error{Code: code, Description: description} }

var (
	// ErrNotFound is a missing client, code, token or connection.
	ErrNotFound = errors.New("oauth record not found")
	// ErrInvalidToken is an access token the MCP endpoint must answer with 401.
	ErrInvalidToken = errors.New("invalid mcp access token")
	// ErrAccountInactive means the account may not grant creative access.
	ErrAccountInactive = errors.New("account cannot grant creative access")
	// ErrRequestInvalid is a consent request package that was altered or expired.
	ErrRequestInvalid = errors.New("authorization request is invalid or expired")
)

// Config assembles the authorization server. BaseURL is PUBLIC_BASE_URL; the
// issuer is BaseURL and the protected resource is BaseURL + "/mcp".
type Config struct {
	BaseURL string
	// Secret is AUTH_TOKEN_SECRET; signing keys are derived from it by purpose.
	Secret string
	Store  Store
	// HTTPClient fetches client metadata documents. Nil means the guarded
	// default, which refuses private addresses.
	HTTPClient *http.Client
	Now        func() time.Time
	Random     io.Reader
	Logger     *slog.Logger
}

// Service is the authorization server.
type Service struct {
	base       string
	resource   string
	store      Store
	accessKey  []byte
	requestKey []byte
	client     *http.Client
	now        func() time.Time
	random     io.Reader
	logger     *slog.Logger

	mu            sync.Mutex
	registrations map[string][]time.Time
}

// New validates the configuration.
func New(cfg Config) (*Service, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" {
		return nil, fmt.Errorf("oauth: base url %q must be an absolute origin", cfg.BaseURL)
	}
	if cfg.Secret == "" || cfg.Store == nil {
		return nil, errors.New("oauth: secret and store are required")
	}
	s := &Service{
		base: base, resource: base + "/mcp", store: cfg.Store,
		accessKey: auth.DerivePurposeKey(cfg.Secret, accessKeyLabel), requestKey: auth.DerivePurposeKey(cfg.Secret, requestKeyLabel),
		client: cfg.HTTPClient, now: cfg.Now, random: cfg.Random, logger: cfg.Logger,
		registrations: map[string][]time.Time{},
	}
	if s.client == nil {
		s.client = guardedClient()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.random == nil {
		s.random = rand.Reader
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

// Issuer is the authorization server identifier, also sent as `iss`.
func (s *Service) Issuer() string { return s.base }

// Resource is the MCP endpoint every access token is bound to.
func (s *Service) Resource() string { return s.resource }

// ResourceMetadataURL is where a 401 points the client.
func (s *Service) ResourceMetadataURL() string {
	return s.base + "/.well-known/oauth-protected-resource/mcp"
}

func (s *Service) randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(s.random, b); err != nil {
		return "", fmt.Errorf("oauth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func digest(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// allowRegistration is a fixed hourly budget per client IP. Counting in
// process memory is enough for the single-process deployment; a restart
// forgets it, which only ever lets a few more registrations through.
func (s *Service) allowRegistration(ip string, now time.Time) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.registrations[ip][:0]
	for _, at := range s.registrations[ip] {
		if now.Sub(at) < time.Hour {
			kept = append(kept, at)
		}
	}
	if len(kept) >= registrationRate {
		s.registrations[ip] = kept
		return time.Hour - now.Sub(kept[0]), false
	}
	s.registrations[ip] = append(kept, now)
	if len(s.registrations) > 10000 {
		// Bound the map: forget IPs that have not registered in the last hour.
		for key, times := range s.registrations {
			if len(times) == 0 || now.Sub(times[len(times)-1]) >= time.Hour {
				delete(s.registrations, key)
			}
		}
	}
	return 0, true
}
