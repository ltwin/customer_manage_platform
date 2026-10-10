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
	// 预留按三个维度的硬上界：48000×5 + 18000×7.3 + 64000×27.2 微美元。
	if q := result.Prepared.Quote(); q.UpperBoundMicros == nil || *q.UpperBoundMicros != 2_112_200 {
		t.Fatalf("upper bound %+v", q)
	}
	settled, err := result.Prepared.PriceUsage(map[string]int64{"text_input_tokens": 25, "image_input_tokens": 0, "image_output_tokens": 3948})
	if err != nil || settled.TotalMicros != 107_511 {
		t.Fatalf("settlement %+v %v", settled, err)
	}

	for name, c := range map[string]struct {
		params string
		list   []gw.GenerationReference
		want   gw.GenerationVerdict
	}{
		"three images":    {`{"count":3}`, nil, ""},
		"unknown ratio":   {`{"aspect_ratio":"7:3"}`, nil, ""},
		"gif reference":   {`{}`, []gw.GenerationReference{refs.ref("gif", "image/gif", gw.GenerationImage)}, gw.GenerationDeny},
		"text reference":  {`{}`, []gw.GenerationReference{refs.ref("note", "text/plain", gw.GenerationText)}, gw.GenerationAllow},
		"tenth reference": {`{}`, slicesRepeat(refs.ref("png", "image/png", gw.GenerationImage), 10), gw.GenerationDeny},
		"ninth reference": {`{"aspect_ratio":"auto","count":4,"quality":"high"}`, slicesRepeat(refs.ref("png", "image/png", gw.GenerationImage), 9), gw.GenerationAllow},
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
	result, err := prepareImage(t, "gpt-image-2", `{"aspect_ratio":"auto","resolution":"4K","count":2,"background":"transparent"}`, refs,
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
	// auto比例不带清晰度；只外发图片，文字参考已在提示词里。
	urls, _ := sent["image_urls"].([]any)
	if sent["size"] != "auto" || sent["resolution"] != nil || sent["n"] != float64(2) || sent["background"] != "transparent" || len(urls) != 1 ||
		urls[0] != "data:image/png;base64,Zmlyc3QtaW1hZ2U=" {
		t.Fatalf("sent %v", sent)
	}

	// 字节与冻结摘要不一致时绝不外发。
	refs["a"] = []byte("tampered!!!")
	calls := 0
	r.submit = func(map[string]any) (int, string) { calls++; return http.StatusOK, `{"id":"x"}` }
	if _, err := a.Submit(t.Context(), gw.GenerationCall{Prepared: result.Prepared, References: refs}); !errors.Is(err, gw.ErrConflict) || !errors.Is(err, gw.ErrNotDispatched) || calls != 0 {
		t.Fatalf("tampered reference: %v calls=%d", err, calls)
	}
}

func TestAICodeWithImageStatusesMapToEvidence(t *testing.T) {
	result, err := prepareImage(t, "gpt-image-2.5-sunburst", `{"count":4}`, imageReferences{})
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
	// 只回了三张也是完成；用量三项齐全才结算。
	r.task = `{"id":"task-9","status":"completed","progress":100,"results":[
		"https://files.example/a/file_1a.png","https://files.example/a/file_2b.png","https://files.example/a/file_3c.png"],
		"usage":{"text_input_tokens":17,"image_input_tokens":0,"image_output_tokens":588,"cost":{"usd":0.016}}}`
	event, err = a.Observe(t.Context(), call)
	if err != nil || event.Observation.State != gw.GenerationCompleted || len(event.Observation.Outputs) != 3 || event.Usage["image_output_tokens"] != 588 {
		t.Fatalf("completed %+v %v", event, err)
	}
	if err := event.Observation.Validate(result.Prepared.Generation()); err != nil {
		t.Fatal(err)
	}
	if out := event.Observation.Outputs[1]; out.ID != "file_2b" || out.Media.FetchURL != "https://files.example/a/file_2b.png" {
		t.Fatalf("output %+v", out)
	}
	r.task = `{"id":"task-9","status":"failed","error":{"code":"content_policy_violation","message":"包含真实人脸"}}`
	event, err = a.Observe(t.Context(), call)
	if err != nil || event.Observation.State != gw.GenerationFailed || event.Observation.ErrorCode != "content_policy_violation" || event.Usage != nil {
		t.Fatalf("failed %+v %v", event, err)
	}
	r.task = ""
	if event, err = a.Cancel(t.Context(), call); err != nil || event.Observation.State != gw.GenerationUnknown {
		t.Fatalf("missing task %+v %v", event, err)
	}
}
