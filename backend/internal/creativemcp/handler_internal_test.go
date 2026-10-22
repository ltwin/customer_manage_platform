package creativemcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
)

func TestLimiterIsPerConnectionAndSlides(t *testing.T) {
	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	l := newLimiter(3, func() time.Time { return now })
	for range 3 {
		if _, ok := l.allow("cxcon_a"); !ok {
			t.Fatal("call within the limit refused")
		}
	}
	wait, ok := l.allow("cxcon_a")
	if ok || wait != 60 {
		t.Fatalf("fourth call: ok=%v wait=%d", ok, wait)
	}
	if _, ok := l.allow("cxcon_b"); !ok {
		t.Fatal("another connection throttled")
	}
	now = now.Add(61 * time.Second)
	if _, ok := l.allow("cxcon_a"); !ok {
		t.Fatal("window did not slide")
	}
}

func TestResultsPutTheConclusionFirst(t *testing.T) {
	plain := success(creativeagent.ExternalResult{Summary: "最近编辑的 1 张画布：", Structured: json.RawMessage(`{"items":[]}`)})
	text := plain.Content[0].(*mcp.TextContent).Text
	if !strings.HasPrefix(text, "最近编辑的 1 张画布：\n\n{") || plain.StructuredContent == nil || plain.IsError {
		t.Fatalf("plain result %+v", plain)
	}
	withImage := success(creativeagent.ExternalResult{Summary: "已附上图片。", Structured: json.RawMessage(`{"image_revision_id":"x"}`),
		Images: []creativeagent.ExternalImage{{MIME: "image/jpeg", Data: []byte{1, 2}}}})
	if len(withImage.Content) != 2 || withImage.StructuredContent != nil {
		t.Fatalf("image result %+v", withImage)
	}
	if img, ok := withImage.Content[1].(*mcp.ImageContent); !ok || img.MIMEType != "image/jpeg" {
		t.Fatalf("image content %+v", withImage.Content[1])
	}
	refused := refusal(&creativeagent.ExternalError{Code: "rate_limited", Message: "调用太频繁", RetryAfter: 12})
	if !refused.IsError || refused.Content[0].(*mcp.TextContent).Text != `{"code":"rate_limited","message":"调用太频繁","retry_after_s":12}` {
		t.Fatalf("refusal %+v", refused.Content[0])
	}
}
