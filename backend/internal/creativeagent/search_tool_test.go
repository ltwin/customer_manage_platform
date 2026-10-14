package creativeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func searchCall(id string, index int, query string) llmgateway.ToolCall {
	return call(id, index, searchWebTool, fmt.Sprintf(`{"query":%q}`, query))
}

// search_web comes with read_webpage. The same query spelled differently is
// answered again without another billed search; long snippets are trimmed so
// the hits reach the model inline.
func TestAssistantSearchesTheWeb(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	fetcher := &stubFetcher{}
	f.service.SetWebFetcher(fetcher)
	queries := []string{"森系 外景", "  森系   外景 ", " ", strings.Repeat("字", searchQueryRunes+1), "慢", "长摘要"}
	results := make([]string, len(queries))
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			if !offered(r, searchWebTool) || !offered(r, readWebpageTool) {
				t.Error("web tools not offered together")
			}
			calls := make([]llmgateway.ToolCall, len(queries))
			for i, q := range queries {
				calls[i] = searchCall(fmt.Sprint("s", i), i, q)
			}
			return scriptedResult(r, calls...), nil
		}
		for i := range queries {
			results[i] = toolResult(r, i)
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "找找森系外景的资料"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	var first struct {
		Success bool        `json:"success"`
		Results []searchHit `json:"results"`
	}
	if err = json.Unmarshal([]byte(results[0]), &first); err != nil || !first.Success || len(first.Results) != 1 || first.Results[0].URL != "https://example.com/guide" {
		t.Fatalf("result %q", results[0])
	}
	if results[1] != results[0] {
		t.Fatalf("not replayed: %q", results[1])
	}
	for i, want := range map[int]string{2: "不能为空", 3: "过长", 4: "搜索超时"} {
		if !strings.Contains(results[i], want) || !strings.Contains(results[i], "creative_tool_refused") {
			t.Fatalf("result %d = %q, want %q", i, results[i], want)
		}
	}
	if len(results[5]) > inlineResultBytes || strings.Contains(results[5], "item_id") || !strings.Contains(results[5], "https://example.com/long/9") || !strings.Contains(results[5], "<b>&") {
		t.Fatalf("long result %d bytes: %.200q", len(results[5]), results[5])
	}
	if strings.Join(fetcher.queries, "|") != "森系 外景|慢|长摘要" {
		t.Fatalf("searched %q", fetcher.queries)
	}
}

func TestAssistantSearchesAreBoundedPerRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	fetcher := &stubFetcher{}
	f.service.SetWebFetcher(fetcher)
	var last string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		if n == 1 {
			calls := make([]llmgateway.ToolCall, searchesPerRun+1)
			for i := range calls {
				calls[i] = searchCall(fmt.Sprint("s", i), i, fmt.Sprint("主题 ", i))
			}
			return scriptedResult(r, calls...), nil
		}
		last = toolResult(r, searchesPerRun)
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "多搜几次"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if len(fetcher.queries) != searchesPerRun || !strings.Contains(last, "已达上限") {
		t.Fatalf("searched %d, last %q", len(fetcher.queries), last)
	}
}

func TestSearchKeyIgnoresSpacingAndCase(t *testing.T) {
	if searchKey("  Sen  Xi\t外景 ") != searchKey("sen xi 外景") || searchKey("森系外景") == searchKey("森系 外景") {
		t.Fatal("search key")
	}
}
