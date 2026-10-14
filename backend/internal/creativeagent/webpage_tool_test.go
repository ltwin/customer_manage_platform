package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/webfetch"
)

// stubFetcher stands in for Firecrawl; webfetch's own tests cover the wire.
type stubFetcher struct {
	mu      sync.Mutex
	calls   []string
	waits   []time.Duration
	queries []string
}

func (s *stubFetcher) Search(_ context.Context, query string, wait time.Duration) ([]webfetch.SearchResult, error) {
	s.mu.Lock()
	s.queries = append(s.queries, query)
	s.waits = append(s.waits, wait)
	s.mu.Unlock()
	switch {
	case strings.Contains(query, "慢"):
		return nil, webfetch.ErrTimeout
	case strings.Contains(query, "长"):
		hits := make([]webfetch.SearchResult, 10)
		for i := range hits {
			hits[i] = webfetch.SearchResult{Title: "结果", URL: fmt.Sprintf("https://example.com/long/%d", i), Description: strings.Repeat("摘要<b>&", 400)}
		}
		return hits, nil
	}
	return []webfetch.SearchResult{{Title: "森系外景攻略", URL: "https://example.com/guide", Description: "## 机位"}}, nil
}

func (s *stubFetcher) Scrape(_ context.Context, rawURL string, wait time.Duration) (webfetch.Page, error) {
	s.mu.Lock()
	s.calls = append(s.calls, rawURL)
	s.waits = append(s.waits, wait)
	s.mu.Unlock()
	switch {
	case strings.HasSuffix(rawURL, "/missing"):
		return webfetch.Page{}, &webfetch.PageStatusError{StatusCode: 404}
	case strings.HasSuffix(rawURL, "/slow"):
		return webfetch.Page{}, webfetch.ErrTimeout
	}
	return webfetch.Page{SourceURL: rawURL, FinalURL: rawURL, Title: "样片", StatusCode: 200, Markdown: "# 样片\n\n<b>森系</b> & 胶片"}, nil
}

func readCall(id string, index int, target string) llmgateway.ToolCall {
	return call(id, index, readWebpageTool, fmt.Sprintf(`{"url":%q}`, target))
}

// read_webpage exists only where a Firecrawl key is configured. A page the
// run already read, or failed to read, is answered again without another
// billed request; every failure is words for the model, not a failed run.
func TestAssistantReadsWebpages(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var withoutKey bool
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		withoutKey = offered(r, readWebpageTool)
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if withoutKey {
		t.Fatal("read_webpage offered without a fetcher")
	}

	fetcher := &stubFetcher{}
	f.service.SetWebFetcher(fetcher)
	targets := []string{
		"https://Example.com:443/a#intro", // read
		"https://example.com/a",           // same page: replayed
		"https://example.com/missing",     // page status refused
		"https://example.com/missing",     // replayed refusal
		"https://www.x.com/post",          // premium route
		"http://192.168.1.1/",             // private address
		"https://example.com/brief.PDF",   // file
		"https://example.com/slow",        // timeout, run goes on
	}
	results := make([]string, len(targets))
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 2 {
			if !offered(r, readWebpageTool) {
				t.Error("read_webpage not offered")
			}
			calls := make([]llmgateway.ToolCall, len(targets))
			for i, target := range targets {
				calls[i] = readCall(fmt.Sprint("w", i), i, target)
			}
			return scriptedResult(r, calls...), nil
		}
		for i := range targets {
			results[i] = toolResult(r, i)
		}
		return scriptedResult(r), nil
	}
	run, err = f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看看这些链接"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || got.State != RunSucceeded {
		t.Fatalf("run=%+v %v", got, err)
	}
	var page webpageAnswer
	if err = json.Unmarshal([]byte(results[0]), &page); err != nil || !page.Success || page.Title != "样片" || page.PageStatusCode != 200 || page.Truncated || page.URL != targets[0] {
		t.Fatalf("result %q", results[0])
	}
	if !strings.Contains(results[0], "<b>森系</b> & 胶片") {
		t.Fatalf("markdown escaped: %q", results[0])
	}
	if results[1] != results[0] || results[3] != results[2] {
		t.Fatalf("not replayed:\n%q\n%q", results[1], results[3])
	}
	for i, want := range map[int]string{2: "状态 404", 4: "暂不支持读取该站点", 5: "公开的 http/https", 6: "暂不支持读取文件", 7: "超时"} {
		if !strings.Contains(results[i], want) || !strings.Contains(results[i], "creative_tool_refused") {
			t.Fatalf("result %d = %q, want %q", i, results[i], want)
		}
	}
	// Different pages are read together, so only the set is fixed.
	slices.Sort(fetcher.calls)
	if strings.Join(fetcher.calls, " ") != "https://Example.com:443/a#intro https://example.com/missing https://example.com/slow" {
		t.Fatalf("fetched %v", fetcher.calls)
	}
	for _, w := range fetcher.waits {
		if w > readWebpageWait || w < 10*time.Second {
			t.Fatalf("wait %v", w)
		}
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND tool_key=$2 AND error_code='creative_tool_refused'", run.ID, readWebpageTool); n != 6 {
		t.Fatalf("refused steps = %d", n)
	}
}

// The run's reads are counted in call order: twenty calls in one answer are
// all read, the twenty-first is refused without a request.
func TestAssistantWebpageReadsAreBoundedPerRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	fetcher := &stubFetcher{}
	f.service.SetWebFetcher(fetcher)
	var last string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			calls := make([]llmgateway.ToolCall, webpageReadsPerRun+1)
			for i := range calls {
				calls[i] = readCall(fmt.Sprint("w", i), i, fmt.Sprintf("https://example.com/p%d", i))
			}
			return scriptedResult(r, calls...), nil
		}
		last = toolResult(r, webpageReadsPerRun)
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "逐个读这些链接"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if len(fetcher.calls) != webpageReadsPerRun || !strings.Contains(last, "已达上限") {
		t.Fatalf("fetched %d, last %q", len(fetcher.calls), last)
	}
}

func TestWebpageKeyIgnoresSpellingOnly(t *testing.T) {
	key := func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return webpageKey(u)
	}
	base := key("https://example.com/")
	for _, same := range []string{"https://EXAMPLE.com", "https://example.com:443/#top", "HTTPS://example.com/"} {
		if key(same) != base {
			t.Fatalf("%s = %s, want %s", same, key(same), base)
		}
	}
	for _, other := range []string{"http://example.com/", "https://example.com:8443/", "https://example.com/?id=1", "https://example.com/a"} {
		if key(other) == base {
			t.Fatalf("%s collides with %s", other, base)
		}
	}
	if key("http://[::1]:80/") != "http://[::1]/" {
		t.Fatalf("ipv6 key %s", key("http://[::1]:80/"))
	}
}

func TestCheckWebpageOnlyStopsPointlessOrPremiumReads(t *testing.T) {
	for target, refused := range map[string]bool{
		"https://mp.weixin.qq.com/s/abc":   false,
		"https://www.zcool.com.cn/work/1":  false,
		"https://example.com/docs.html":    false,
		"http://8.8.8.8/":                  false,
		"http://localhost:8080/":           true,
		"http://app.localhost/":            true,
		"http://127.0.0.1/":                true,
		"http://10.0.0.8/":                 true,
		"http://169.254.169.254/latest":    true,
		"http://[::1]/":                    true,
		"http://0.0.0.0/":                  true,
		"https://twitter.com/a":            true,
		"https://cn.linkedin.com/in/a":     true,
		"https://notx.com/":                false,
		"https://example.com/a.ZIP":        true,
		"https://example.com/report.xlsx":  true,
		"https://example.com/pdf/overview": false,
	} {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		var public creativeops.Public
		if got := checkWebpage(u); errors.As(got, &public) != refused {
			t.Errorf("%s: err = %v, want refused=%v", target, got, refused)
		}
	}
}

// However the page is written, the result stays under the run's result bound
// and stays valid UTF-8; HTML characters are not inflated by escaping.
func TestWebpageResultFitsTheResultBound(t *testing.T) {
	for name, md := range map[string]string{
		"chinese and html": strings.Repeat("摄影<作品>&\"引号\"\n", 8000),
		"control bytes":    strings.Repeat("\x01", 200<<10),
		"short":            "<p>a & b</p>",
	} {
		out, err := webpageResult("https://example.com/", webfetch.Page{StatusCode: 200, Markdown: md})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var page webpageAnswer
		if len(out) > webpageResultBytes || !utf8.ValidString(out) || json.Unmarshal([]byte(out), &page) != nil {
			t.Fatalf("%s: %d bytes", name, len(out))
		}
		if page.Truncated != (len(md) > len(page.Markdown)) || !strings.HasPrefix(md, page.Markdown) {
			t.Fatalf("%s: truncated=%v kept %d of %d", name, page.Truncated, len(page.Markdown), len(md))
		}
		if strings.Contains(out, "\\u003c") {
			t.Fatalf("%s: html escaped", name)
		}
	}
}
