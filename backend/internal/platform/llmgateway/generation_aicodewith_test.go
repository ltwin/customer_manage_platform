package llmgateway_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

type imageReferences map[string][]byte

func (r imageReferences) OpenGenerationReference(_ context.Context, ref gw.GenerationReference) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r[ref.RevisionID])), nil
}

// LinkGenerationReference 模拟存储签名：地址只由参考身份决定。
func (r imageReferences) LinkGenerationReference(_ context.Context, ref gw.GenerationReference) (string, error) {
	return "https://refs.example/" + ref.RevisionID, nil
}

// bytesOnly 只能打开字节、不能签地址，模拟本地存储驱动。
type bytesOnly struct{ gw.GenerationReferenceOpener }

func (r imageReferences) ResolveGenerationMedia(_ context.Context, ref gw.GenerationReference) (gw.GenerationMediaMetadata, error) {
	return gw.GenerationMediaMetadata{RevisionID: ref.RevisionID, Digest: ref.Digest, ExtractorVersion: "test", Kind: ref.Kind, MIME: ref.MIME, ByteSize: ref.ByteSize}, nil
}

func (r imageReferences) ref(id, mime string, kind gw.GenerationKind) gw.GenerationReference {
	sum := sha256.Sum256(r[id])
	return gw.GenerationReference{Role: "reference", RevisionID: id, Digest: hex.EncodeToString(sum[:]), Kind: kind, MIME: mime, ByteSize: int64(len(r[id]))}
}

func aicodewithPolicy(t *testing.T, model string) gw.AICodeWithImagePolicy {
	t.Helper()
	policies, err := gw.AICodeWithImagePolicies("USD")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range policies {
		if p.Target.ModelKey == model {
			return p
		}
	}
	t.Fatalf("no policy for %s", model)
	return gw.AICodeWithImagePolicy{}
}

func prepareImage(t *testing.T, model, parameters string, refs imageReferences, list ...gw.GenerationReference) (gw.GenerationPolicyResult, error) {
	t.Helper()
	request := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: model, Kind: gw.GenerationImage, Mode: gw.AICodeWithImageMode,
		Input: gw.GenerationInput{Prompt: "逆光人像", References: list, Parameters: json.RawMessage(parameters)}}
	facts, err := gw.ReadGenerationFacts(t.Context(), request, refs)
	if err != nil {
		return gw.GenerationPolicyResult{}, err
	}
	return aicodewithPolicy(t, model).Policy.Prepare(request, facts)
}

func TestAICodeWithImagePolicyDefaultsLimitsAndBound(t *testing.T) {
	refs := imageReferences{"png": []byte("png-bytes"), "gif": []byte("gif-bytes"), "note": []byte("说明")}
	result, err := prepareImage(t, "gpt-image-2.5-flare", `{}`, refs)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("defaults: %+v %v", result, err)
	}
	var params map[string]any
	if err := json.Unmarshal(result.Prepared.Generation().Snapshot().Request.Input.Parameters, &params); err != nil {
		t.Fatal(err)
	}
	if params["aspect_ratio"] != "1:1" || params["resolution"] != "2K" || params["quality"] != "medium" || params["count"] != float64(1) {
		t.Fatalf("defaults %v", params)
	}
	// 按张计费：预留按最贵档（4K）保守取上界，估算与结算按所选清晰度；失败不扣费。
	if q := result.Prepared.Quote(); q.UpperBoundMicros == nil || *q.UpperBoundMicros != 160_000 || q.EstimateMicros != 80_000 {
		t.Fatalf("upper bound %d %+v", *q.UpperBoundMicros, q)
	}
	for images, want := range map[int64]int64{1: 80_000, 0: 0} {
		settled, err := result.Prepared.PriceUsage(map[string]int64{"images": images})
		if err != nil || settled.TotalMicros != want {
			t.Fatalf("settlement of %d images %+v %v", images, settled, err)
		}
	}
	// 2.6按画质加乘，2.5的画质被中转丢弃、不加价。
	for model, c := range map[string]struct {
		params string
		want   int64
	}{
		"gpt-image-2.6-sunburst": {`{"resolution":"4K","quality":"max"}`, 480_000},
		"gpt-image-2.6-flare":    {`{"resolution":"1K","quality":"low"}`, 20_000},
		"gpt-image-2.5-sunburst": {`{"resolution":"1K","quality":"high"}`, 40_000},
	} {
		result, err := prepareImage(t, model, c.params, refs)
		if err != nil || result.Verdict != gw.GenerationAllow || result.Prepared.Quote().EstimateMicros != c.want {
			t.Fatalf("%s %s: %+v %v", model, c.params, result, err)
		}
	}
	if result, err := prepareImage(t, "gpt-image-2.5-flare", `{"quality":"xhigh"}`, refs); err == nil && result.Verdict == gw.GenerationAllow {
		t.Fatal("2.5 accepted a 2.6-only quality")
	}

	for name, c := range map[string]struct {
		params string
		list   []gw.GenerationReference
		want   gw.GenerationVerdict
	}{
		"two images":      {`{"count":2}`, nil, ""},
		"unknown ratio":   {`{"aspect_ratio":"7:3"}`, nil, ""},
		"gif reference":   {`{}`, []gw.GenerationReference{refs.ref("gif", "image/gif", gw.GenerationImage)}, gw.GenerationDeny},
		"text reference":  {`{}`, []gw.GenerationReference{refs.ref("note", "text/plain", gw.GenerationText)}, gw.GenerationAllow},
		"tenth reference": {`{}`, slicesRepeat(refs.ref("png", "image/png", gw.GenerationImage), 10), gw.GenerationDeny},
		"ninth reference": {`{"aspect_ratio":"auto","quality":"high"}`, slicesRepeat(refs.ref("png", "image/png", gw.GenerationImage), 9), gw.GenerationAllow},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := prepareImage(t, "gpt-image-2", c.params, refs, c.list...)
			if c.want == "" {
				if err == nil && result.Verdict == gw.GenerationAllow {
					t.Fatal("accepted an unsupported parameter")
				}
				return
			}
			if err != nil || result.Verdict != c.want {
				t.Fatalf("verdict %+v %v", result, err)
			}
		})
	}
}

func slicesRepeat(r gw.GenerationReference, n int) []gw.GenerationReference {
	out := make([]gw.GenerationReference, n)
	for i := range out {
		out[i] = r
	}
	return out
}

type relay struct {
	t      *testing.T
	submit func(body map[string]any) (int, string)
	task   string
}

func (r *relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Authorization") != "Bearer relay-key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case req.Method == http.MethodPost && req.URL.Path == "/v1/images/generations":
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			r.t.Error(err)
		}
		status, answer := r.submit(body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/tasks/"):
		if r.task == "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"task not found"}`)
			return
		}
		_, _ = io.WriteString(w, r.task)
	default:
		w.WriteHeader(http.StatusTeapot)
	}
}

func relayAdapter(t *testing.T, r *relay) *gw.AICodeWithImages {
	t.Helper()
	server := httptest.NewTLSServer(r)
	t.Cleanup(server.Close)
	a, err := gw.NewAICodeWithImages(server.Client(), server.URL, "relay-key")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAICodeWithImageSubmitSendsVerifiedReferencesOnly(t *testing.T) {
	refs := imageReferences{"a": []byte("first-image"), "note": []byte("说明")}
	result, err := prepareImage(t, "gpt-image-2", `{"aspect_ratio":"auto","resolution":"4K","background":"transparent"}`, refs,
		refs.ref("a", "image/png", gw.GenerationImage), refs.ref("note", "text/plain", gw.GenerationText))
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("prepare %+v %v", result, err)
	}
	var sent map[string]any
	r := &relay{t: t, submit: func(body map[string]any) (int, string) {
		sent = body
		return http.StatusOK, `{"id":"task-1","status":"processing","progress":1}`
	}}
	a := relayAdapter(t, r)
	event, err := a.Submit(t.Context(), gw.GenerationCall{Prepared: result.Prepared, References: refs})
	if err != nil || event.Observation.State != gw.GenerationAccepted || event.Observation.TaskID != "task-1" {
		t.Fatalf("submit %+v %v", event, err)
	}
	// auto比例不带清晰度；只外发图片地址，文字参考已在提示词里；中转一次一张，不发n。
	urls, _ := sent["image_urls"].([]any)
	if sent["size"] != "auto" || sent["resolution"] != nil || sent["n"] != nil || sent["background"] != "transparent" || len(urls) != 1 ||
		urls[0] != "https://refs.example/a" {
		t.Fatalf("sent %v", sent)
	}

	// 存储签不出公网地址时绝不外发，也不退回data URL。
	calls := 0
	r.submit = func(map[string]any) (int, string) { calls++; return http.StatusOK, `{"id":"x"}` }
	if _, err := a.Submit(t.Context(), gw.GenerationCall{Prepared: result.Prepared, References: bytesOnly{refs}}); !errors.Is(err, gw.ErrUnavailable) || !errors.Is(err, gw.ErrNotDispatched) || calls != 0 {
		t.Fatalf("unlinkable reference: %v calls=%d", err, calls)
	}
}

func TestAICodeWithImageStatusesMapToEvidence(t *testing.T) {
	result, err := prepareImage(t, "gpt-image-2.6-sunburst", `{"quality":"xhigh"}`, imageReferences{})
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("prepare %+v %v", result, err)
	}
	r := &relay{t: t}
	a := relayAdapter(t, r)
	call := gw.GenerationCall{Prepared: result.Prepared}

	for status, code := range map[int]string{400: "invalid_parameter", 402: "insufficient_balance", 503: "no_available_channel"} {
		r.submit = func(map[string]any) (int, string) { return status, `{"error":{"code":"x"}}` }
		event, err := a.Submit(t.Context(), call)
		if err != nil || event.Observation.State != gw.GenerationRejected || event.Observation.ErrorCode != code || event.Observation.Validate(result.Prepared.Generation()) != nil {
			t.Fatalf("status %d: %+v %v", status, event, err)
		}
	}
	// 5xx 不能证明未受理，交给运行时按受理未知处理。
	r.submit = func(map[string]any) (int, string) { return http.StatusBadGateway, `` }
	if _, err := a.Submit(t.Context(), call); err == nil {
		t.Fatal("a 502 was treated as evidence")
	}

	call.TaskID = "task-9"
	r.task = `{"id":"task-9","status":"processing","progress":40}`
	event, err := a.Observe(t.Context(), call)
	if err != nil || event.Observation.State != gw.GenerationRunning || *event.Observation.Progress != 0.4 {
		t.Fatalf("running %+v %v", event, err)
	}
	// 2026-11-09实测形态：结果在result_data，不带用量；按张数结算。
	r.task = `{"id":"task-9","result_data":[{"url":"https://api.aicodewith.com/media/v/outputs/task-9/0.png"}],"status":"completed"}`
	event, err = a.Observe(t.Context(), call)
	if err != nil || event.Observation.State != gw.GenerationCompleted || len(event.Observation.Outputs) != 1 || event.Usage["images"] != 1 {
		t.Fatalf("completed %+v %v", event, err)
	}
	if err := event.Observation.Validate(result.Prepared.Generation()); err != nil {
		t.Fatal(err)
	}
	if out := event.Observation.Outputs[0]; out.ID != "task-9_0" || out.Media.FetchURL != "https://api.aicodewith.com/media/v/outputs/task-9/0.png" {
		t.Fatalf("output %+v", out)
	}
	// 失败与取消都不扣费。
	for task, code := range map[string]string{
		`{"id":"task-9","status":"failed","result_data":[],"error":{"code":"content_policy_violation","message":"包含真实人脸"}}`: "content_policy_violation",
		`{"id":"task-9","status":"cancelled","result_data":[]}`:                                                             "provider_cancelled",
	} {
		r.task = task
		event, err = a.Observe(t.Context(), call)
		if err != nil || event.Observation.State != gw.GenerationFailed || event.Observation.ErrorCode != code || event.Usage["images"] != 0 || len(event.Usage) != 1 {
			t.Fatalf("failed %+v %v", event, err)
		}
	}
	r.task = ""
	if event, err = a.Cancel(t.Context(), call); err != nil || event.Observation.State != gw.GenerationUnknown {
		t.Fatalf("missing task %+v %v", event, err)
	}
}
