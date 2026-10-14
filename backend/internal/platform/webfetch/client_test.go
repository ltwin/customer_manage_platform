package webfetch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return newClient(srv.URL, "fc-test", srv.Client())
}

func TestScrapeSendsFixedOptions(t *testing.T) {
	var got map[string]any
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"success":true,"data":{"markdown":"# 标题\n正文","metadata":{"title":["","标题"],"sourceURL":"https://example.com/a","url":"https://example.com/b","statusCode":200,"contentType":"text/html; charset=utf-8","cacheState":"hit","provider":{"name":"x"}}}}`)
	}))
	defer srv.Close()
	page, err := newClient(srv.URL, "fc-test", srv.Client()).Scrape(t.Context(), "https://example.com/a", 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer fc-test" || path != "/v2/scrape" {
		t.Fatalf("auth=%q path=%q", auth, path)
	}
	want := map[string]any{"url": "https://example.com/a", "formats": []any{"markdown"}, "onlyMainContent": true, "maxAge": float64(600000), "timeout": float64(55000), "skipTlsVerification": false, "parsers": []any{}}
	if len(got) != len(want) {
		t.Fatalf("request %v", got)
	}
	for k, v := range want {
		if b1, _ := json.Marshal(got[k]); string(b1) != mustJSON(v) {
			t.Fatalf("%s = %v, want %v", k, got[k], v)
		}
	}
	if page.Title != "标题" || page.FinalURL != "https://example.com/b" || page.StatusCode != 200 || page.CacheState != "hit" || string(page.Provider) != `{"name":"x"}` || !strings.Contains(page.Markdown, "正文") {
		t.Fatalf("page %+v", page)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func ok(meta string) string {
	return `{"success":true,"data":{"markdown":"正文","metadata":` + meta + `}}`
}

func TestScrapeMapsAnswers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
		page   int // expected PageStatusError code; -1 when not a page status
	}{
		{"loaded", 200, ok(`{"statusCode":200}`), nil, -1},
		{"not modified", 200, ok(`{"statusCode":304}`), nil, -1},
		{"provider string", 200, ok(`{"statusCode":200,"provider":"fire-engine"}`), nil, -1},
		{"redirect stub", 200, ok(`{"statusCode":301}`), nil, 301},
		{"forbidden page", 200, ok(`{"statusCode":403,"error":"blocked"}`), nil, 403},
		{"missing status", 200, ok(`{}`), nil, 0},
		{"dns failure", 200, `{"success":false,"code":"SCRAPE_DNS_RESOLUTION_ERROR"}`, ErrUnreachable, -1},
		{"blank markdown", 200, `{"success":true,"data":{"markdown":" \n","metadata":{"statusCode":200}}}`, ErrUnsupported, -1},
		{"pdf", 200, ok(`{"statusCode":200,"contentType":"application/pdf"}`), ErrUnsupported, -1},
		{"auth", 401, `{"success":false}`, ErrProviderAuth, -1},
		{"forbidden key", 403, `{"success":false}`, ErrProviderAuth, -1},
		{"quota", 402, `{"success":false}`, ErrQuota, -1},
		{"timeout", 408, `{"success":false,"code":"SCRAPE_TIMEOUT"}`, ErrTimeout, -1},
		{"rate", 429, `{"success":false}`, ErrRateLimited, -1},
		{"site", 500, `{"success":false,"code":"SCRAPE_SITE_ERROR"}`, ErrUnreachable, -1},
		{"ssl", 500, `{"success":false,"code":"SCRAPE_SSL_ERROR"}`, ErrUnreachable, -1},
		{"engines", 500, `{"success":false,"code":"SCRAPE_ALL_ENGINES_FAILED"}`, ErrUnreachable, -1},
		{"file", 500, `{"success":false,"code":"SCRAPE_UNSUPPORTED_FILE_ERROR"}`, ErrUnsupported, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := serve(t, tc.status, tc.body).Scrape(t.Context(), "https://example.com/", 30*time.Second)
			var status *PageStatusError
			switch {
			case tc.page >= 0:
				if !errors.As(err, &status) || status.StatusCode != tc.page {
					t.Fatalf("err = %v, want page status %d", err, tc.page)
				}
			case tc.want == nil:
				if err != nil || page.Markdown != "正文" || page.SourceURL != "https://example.com/" {
					t.Fatalf("page %+v err %v", page, err)
				}
			default:
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
			}
		})
	}
}

// Unknown failures stay plain errors: the caller treats them as unexplained.
func TestScrapeLeavesUnknownFailuresUnnamed(t *testing.T) {
	named := []error{ErrTimeout, ErrQuota, ErrRateLimited, ErrUnreachable, ErrUnsupported, ErrProviderAuth}
	for _, tc := range []struct {
		status int
		body   string
	}{{500, `{"success":false,"code":"UNKNOWN_ERROR"}`}, {502, `bad gateway`}, {200, `{not json`}} {
		_, err := serve(t, tc.status, tc.body).Scrape(t.Context(), "https://example.com/", 30*time.Second)
		if err == nil {
			t.Fatalf("%d %s: no error", tc.status, tc.body)
		}
		for _, n := range named {
			if errors.Is(err, n) {
				t.Fatalf("%d %s: %v is named %v", tc.status, tc.body, err, n)
			}
		}
	}
}

func TestScrapeBoundsTheAnswer(t *testing.T) {
	big := `{"success":true,"data":{"markdown":"` + strings.Repeat("a", maxResponseBytes) + `"}}`
	if _, err := serve(t, 200, big).Scrape(t.Context(), "https://example.com/", 30*time.Second); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

// Our own deadline is a slow page, never the run's deadline; the caller's
// context ending is passed back as is so the run stops.
func TestScrapeTimeouts(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := newClient(srv.URL, "fc-test", srv.Client())

	_, err := c.Scrape(t.Context(), "https://example.com/", 50*time.Millisecond)
	if !errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sub-deadline err = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err = c.Scrape(ctx, "https://example.com/", time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent err = %v", err)
	}
}

func TestFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c, err := FromEnv(env(nil)); c != nil || err != nil {
		t.Fatalf("unset: %v %v", c, err)
	}
	c, err := FromEnv(env(map[string]string{"FIRECRAWL_API_KEY": "fc-x"}))
	if err != nil || c.baseURL != defaultBaseURL {
		t.Fatalf("default: %+v %v", c, err)
	}
	for _, base := range []string{"http://api.firecrawl.dev", "api.firecrawl.dev", "https://u:p@host", "https://"} {
		if _, err := FromEnv(env(map[string]string{"FIRECRAWL_API_KEY": "fc-secret", "FIRECRAWL_BASE_URL": base})); err == nil || strings.Contains(err.Error(), "fc-secret") {
			t.Fatalf("%s: err = %v", base, err)
		}
	}
}
