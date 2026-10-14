// Package webfetch reads one public web page through Firecrawl Cloud and turns
// the answer into a clean page or a named failure. It knows nothing about runs,
// accounts or the database; what to do with a page is the caller's call.
package webfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.firecrawl.dev"
	// maxAge lets Firecrawl answer from its own ten-minute cache. It only saves
	// time: a cached page is billed like a fresh one.
	maxAge = 10 * time.Minute
	// providerHeadroom is how much longer we wait than Firecrawl does, so its
	// own timeout answer arrives before our deadline cuts the request.
	providerHeadroom = 5 * time.Second
	maxResponseBytes = 8 << 20
)

// Page is what one scrape produced, already validated. Markdown is untrimmed;
// truncation is the caller's call because only it knows its result budget.
type Page struct {
	SourceURL   string
	FinalURL    string
	Title       string
	StatusCode  int
	ContentType string
	Markdown    string
	CacheState  string
	// Provider is kept raw: its shape is the vendor's and only goes to logs.
	Provider json.RawMessage
}

var (
	ErrTimeout      = errors.New("webfetch: page did not load in time")
	ErrQuota        = errors.New("webfetch: provider credits exhausted")
	ErrRateLimited  = errors.New("webfetch: provider rate limited")
	ErrUnreachable  = errors.New("webfetch: page could not be opened")
	ErrUnsupported  = errors.New("webfetch: content is not a web page")
	ErrProviderAuth = errors.New("webfetch: provider rejected the credential")
)

// PageStatusError is a page that answered but did not load cleanly.
type PageStatusError struct {
	// StatusCode is 0 when the vendor reported none.
	StatusCode int
	// Detail is the vendor's metadata.error, for logs only.
	Detail string
}

func (e *PageStatusError) Error() string {
	return fmt.Sprintf("webfetch: page answered status %d", e.StatusCode)
}

// Client calls Firecrawl's scrape endpoint. It never retries: a retry is a
// second billed page, and whether that is worth it is the caller's decision.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// FromEnv returns nil, nil when FIRECRAWL_API_KEY is unset: the deployment
// simply does not offer the tool.
func FromEnv(getenv func(string) string) (*Client, error) {
	key := strings.TrimSpace(getenv("FIRECRAWL_API_KEY"))
	if key == "" {
		return nil, nil
	}
	base := strings.TrimSpace(getenv("FIRECRAWL_BASE_URL"))
	if base == "" {
		base = defaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("FIRECRAWL_BASE_URL must be an absolute https URL")
	}
	return newClient(strings.TrimSuffix(u.String(), "/"), key, &http.Client{}), nil
}

func newClient(baseURL, apiKey string, hc *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: hc}
}

type scrapeRequest struct {
	URL                 string   `json:"url"`
	Formats             []string `json:"formats"`
	OnlyMainContent     bool     `json:"onlyMainContent"`
	MaxAge              int64    `json:"maxAge"`
	Timeout             int64    `json:"timeout"`
	SkipTLSVerification bool     `json:"skipTlsVerification"`
	Parsers             []string `json:"parsers"`
}

type scrapeResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		Markdown string   `json:"markdown"`
		Metadata metadata `json:"metadata"`
	} `json:"data"`
}

type metadata struct {
	// Title is a string or an array of strings depending on the page.
	Title       json.RawMessage `json:"title"`
	SourceURL   string          `json:"sourceURL"`
	URL         string          `json:"url"`
	StatusCode  *int            `json:"statusCode"`
	ContentType string          `json:"contentType"`
	Error       string          `json:"error"`
	CacheState  string          `json:"cacheState"`
	Provider    json.RawMessage `json:"provider"`
}

// Scrape reads one page within wait. Firecrawl is told to give up 5 seconds
// earlier, so a slow page comes back as its own timeout answer. Our deadline
// running out is ErrTimeout, deliberately not context.DeadlineExceeded: the
// page was slow, the caller's run was not. A caller whose own context ended
// gets that context's error back unchanged.
func (c *Client) Scrape(ctx context.Context, rawURL string, wait time.Duration) (Page, error) {
	body, err := json.Marshal(scrapeRequest{
		URL:     rawURL,
		Formats: []string{"markdown"}, OnlyMainContent: true,
		MaxAge:  maxAge.Milliseconds(),
		Timeout: max(wait-providerHeadroom, time.Second).Milliseconds(),
		Parsers: []string{},
	})
	if err != nil {
		return Page{}, err
	}
	sub, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(sub, http.MethodPost, c.baseURL+"/v2/scrape", bytes.NewReader(body))
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Page{}, interrupted(ctx, sub, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Page{}, interrupted(ctx, sub, err)
	}
	if len(raw) > maxResponseBytes {
		return Page{}, fmt.Errorf("%w: response over %d bytes", ErrUnsupported, maxResponseBytes)
	}
	return decode(resp.StatusCode, raw, rawURL)
}

// interrupted tells the caller's own end from our sub-deadline.
func interrupted(ctx, sub context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if sub.Err() != nil {
		return ErrTimeout
	}
	return fmt.Errorf("webfetch: request failed: %w", err)
}

func decode(status int, raw []byte, rawURL string) (Page, error) {
	var out scrapeResponse
	jsonErr := json.Unmarshal(raw, &out)
	switch {
	case status == http.StatusOK:
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return Page{}, ErrProviderAuth
	case status == http.StatusPaymentRequired:
		return Page{}, ErrQuota
	case status == http.StatusRequestTimeout:
		return Page{}, ErrTimeout
	case status == http.StatusTooManyRequests:
		return Page{}, ErrRateLimited
	case status == http.StatusInternalServerError && jsonErr == nil:
		switch out.Code {
		case "SCRAPE_SITE_ERROR", "SCRAPE_DNS_RESOLUTION_ERROR", "SCRAPE_SSL_ERROR", "SCRAPE_ALL_ENGINES_FAILED":
			return Page{}, fmt.Errorf("%w (code %s)", ErrUnreachable, out.Code)
		case "SCRAPE_UNSUPPORTED_FILE_ERROR":
			return Page{}, fmt.Errorf("%w (code %s)", ErrUnsupported, out.Code)
		}
		return Page{}, fmt.Errorf("webfetch: provider answered %d (code %s)", status, out.Code)
	default:
		return Page{}, fmt.Errorf("webfetch: provider answered %d", status)
	}
	if jsonErr != nil {
		return Page{}, fmt.Errorf("webfetch: malformed provider answer: %w", jsonErr)
	}
	if !out.Success {
		return Page{}, fmt.Errorf("%w (code %s)", ErrUnreachable, out.Code)
	}
	m := out.Data.Metadata
	// Only 2xx and 304 count as a page that loaded; anything else, or no status
	// at all, is an error page or a redirect stub whatever its text says.
	if !loaded(m.StatusCode) {
		code := 0
		if m.StatusCode != nil {
			code = *m.StatusCode
		}
		return Page{}, &PageStatusError{StatusCode: code, Detail: m.Error}
	}
	if m.ContentType != "" {
		media, _, err := mime.ParseMediaType(m.ContentType)
		if err != nil || media != "text/html" && media != "application/xhtml+xml" && media != "text/plain" {
			return Page{}, fmt.Errorf("%w: content type %q", ErrUnsupported, m.ContentType)
		}
	}
	if strings.TrimSpace(out.Data.Markdown) == "" {
		return Page{}, fmt.Errorf("%w: empty markdown", ErrUnsupported)
	}
	source := m.SourceURL
	if source == "" {
		source = rawURL
	}
	return Page{
		SourceURL: source, FinalURL: m.URL, Title: title(m.Title),
		StatusCode: *m.StatusCode, ContentType: m.ContentType, Markdown: out.Data.Markdown,
		CacheState: m.CacheState, Provider: m.Provider,
	}, nil
}

func loaded(status *int) bool {
	if status == nil {
		return false
	}
	return *status >= 200 && *status < 300 || *status == http.StatusNotModified
}

// title takes the first non-empty title whether the vendor sent one or many.
func title(raw json.RawMessage) string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return strings.TrimSpace(one)
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, t := range many {
			if t = strings.TrimSpace(t); t != "" {
				return t
			}
		}
	}
	return ""
}
