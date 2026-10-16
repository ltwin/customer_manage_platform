package llmgateway_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func runningHubPolicy(t *testing.T, model string) gw.ImagePolicy {
	t.Helper()
	ps, err := gw.RunningHubImagePolicies("USD")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Definition.Profile.ModelKey == model {
			return p
		}
	}
	t.Fatalf("missing model %s", model)
	return gw.ImagePolicy{}
}
func TestRunningHubUploadAndTaskProtocol(t *testing.T) {
	refs := imageReferences{"ref": []byte("original-image")}
	var calls []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Error("missing credential")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v2/media/upload/binary":
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = f.Close() }()
			data, _ := io.ReadAll(f)
			if string(data) != "original-image" {
				t.Error("wrong reference")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"type":"image","download_url":"https://rh-images-switch-1252422369.cos.ap-guangzhou.myqcloud.com/input/ref.png"}}`))
		case "/openapi/v2/rhart-image-g-2-official/image-to-image":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["aspectRatio"] != "1:1" || body["resolution"] != "2k" || body["quality"] != "medium" || body["imageUrls"] == nil {
				t.Errorf("body %v", body)
			}
			if body["n"] != nil || body["background"] != nil {
				t.Errorf("unsupported fields %v", body)
			}
			_, _ = w.Write([]byte(`{"taskId":"task-1","status":"RUNNING"}`))
		case "/openapi/v2/query":
			_, _ = w.Write([]byte(`{"taskId":"task-1","status":"SUCCESS","results":[{"url":"https://rh-images-1252422369.cos.ap-beijing.myqcloud.com/output/image.jpg","outputType":"jpg"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	a, err := gw.NewRunningHubImages(server.Client(), server.URL, func() (string, bool) { return "key", true })
	if err != nil {
		t.Fatal(err)
	}
	policy := runningHubPolicy(t, "gpt-image-2")
	request := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "gpt-image-2", Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "portrait", References: []gw.GenerationReference{refs.ref("ref", "image/png", gw.GenerationImage)}, Parameters: json.RawMessage(`{}`)}}
	facts, err := gw.ReadGenerationFacts(t.Context(), request, refs)
	if err != nil {
		t.Fatal(err)
	}
	result, err := policy.Policy.Prepare(request, facts)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("prepare %v %+v", err, result)
	}
	call := gw.GenerationCall{Prepared: result.Prepared, References: refs}
	event, err := a.Submit(t.Context(), call)
	if err != nil || event.Observation.TaskID != "task-1" {
		t.Fatalf("submit %v %+v", err, event)
	}
	call.TaskID = event.Observation.TaskID
	event, err = a.Observe(t.Context(), call)
	if err != nil || event.Observation.State != gw.GenerationCompleted || len(event.Observation.Outputs) != 1 {
		t.Fatalf("observe %v %+v", err, event)
	}
	if len(event.Usage) != 0 {
		t.Fatal("query has no billing evidence; must not invent usage")
	}
	if len(calls) != 3 {
		t.Fatalf("calls %v", calls)
	}
}

func TestRunningHubRejectsUnsupportedParameters(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "nano-banana-pro", "nano-banana-2", "nano-banana-2.1"} {
		p := runningHubPolicy(t, model)
		for _, params := range []string{`{"count":4}`, `{"background":"transparent"}`, `{"unknown":true}`} {
			r := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: model, Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "p", Parameters: json.RawMessage(params)}}
			facts, err := gw.ReadGenerationFacts(t.Context(), r, imageReferences{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Policy.Prepare(r, facts)
			if err == nil && got.Verdict == gw.GenerationAllow {
				t.Fatalf("accepted %s %s", model, params)
			}
		}
	}
}

func TestRunningHubModelEndpoints(t *testing.T) {
	for _, tc := range []struct{ model, path, edit string }{
		{"gpt-image-2", "rhart-image-g-2-official", "image-to-image"},
		{"nano-banana-pro", "rhart-image-n-pro-official", "edit"},
		{"nano-banana-2", "rhart-image-n-g31-flash-official", "image-to-image"},
		{"nano-banana-2.1", "rhart-image-n-nb21", "image-to-image"},
	} {
		for _, reference := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reference=%v", tc.model, reference), func(t *testing.T) {
				refs := imageReferences{"ref": []byte("pixels")}
				endpoint := "text-to-image"
				if reference {
					endpoint = tc.edit
				}
				submits := 0
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/openapi/v2/media/upload/binary" {
						_, _ = io.WriteString(w, `{"code":0,"data":{"type":"image","download_url":"https://rh-hk-images-switch.xiaoyaoyou.com/reference.png"}}`)
						return
					}
					if r.URL.Path != "/openapi/v2/"+tc.path+"/"+endpoint {
						t.Errorf("endpoint %s", r.URL.Path)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if tc.model != "gpt-image-2" && body["quality"] != nil {
						t.Error("Nano quality leaked")
					}
					if body["resolution"] != "2k" || body["aspectRatio"] != "1:1" || body["prompt"] != "p" {
						t.Errorf("parameters changed: %v", body)
					}
					if reference {
						urls, ok := body["imageUrls"].([]any)
						if !ok || len(urls) != 1 || urls[0] != "https://rh-hk-images-switch.xiaoyaoyou.com/reference.png" {
							t.Errorf("reference mapping: %v", body["imageUrls"])
						}
					} else if body["imageUrls"] != nil {
						t.Error("text generation must omit references")
					}
					submits++
					_, _ = io.WriteString(w, `{"taskId":"task","status":"QUEUED"}`)
				}))
				defer server.Close()
				policy := runningHubPolicy(t, tc.model)
				req := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: tc.model, Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "p", Parameters: json.RawMessage(`{}`)}}
				if reference {
					req.Input.References = []gw.GenerationReference{refs.ref("ref", "image/png", gw.GenerationImage)}
				}
				facts, err := gw.ReadGenerationFacts(t.Context(), req, refs)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := policy.Policy.Prepare(req, facts)
				if err != nil {
					t.Fatal(err)
				}
				adapter, err := gw.NewRunningHubImages(server.Client(), server.URL, func() (string, bool) { return "key", true })
				if err != nil {
					t.Fatal(err)
				}
				event, err := adapter.Submit(t.Context(), gw.GenerationCall{Prepared: prepared.Prepared, References: refs})
				if err != nil || event.Observation.State != gw.GenerationRunning || submits != 1 {
					t.Fatalf("submit %v %+v calls=%d", err, event, submits)
				}
			})
		}
	}
}

func TestRunningHubFailureAndOutputValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		state      gw.GenerationObservationState
		code       string
		invalid    bool
	}{
		{"credential", `unauthorized`, 401, gw.GenerationRejected, "credential_refused", false},
		{"balance", `payment`, 402, gw.GenerationRejected, "insufficient_balance", false},
		{"invalid", `{"errorCode":"1007"}`, 400, gw.GenerationRejected, "invalid_parameter", false},
		{"ambiguous", `{"errorCode":"1005"}`, 503, "", "", true},
		{"terminated", `{"taskId":"t","status":"FAILED","errorCode":"1006"}`, 200, gw.GenerationFailed, "execution_timeout", false},
		{"upload_host_not_output", `{"taskId":"t","status":"SUCCESS","results":[{"url":"https://rh-hk-images-switch.xiaoyaoyou.com/result.png","outputType":"png"}]}`, 200, "", "", true},
		{"unsafe_result", `{"taskId":"t","status":"SUCCESS","results":[{"url":"https://evil.invalid/x.png","outputType":"png"}]}`, 200, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			policy := runningHubPolicy(t, "nano-banana-2")
			req := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "nano-banana-2", Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "p", Parameters: json.RawMessage(`{}`)}}
			facts, err := gw.ReadGenerationFacts(t.Context(), req, imageReferences{})
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := policy.Policy.Prepare(req, facts)
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := gw.NewRunningHubImages(server.Client(), server.URL, func() (string, bool) { return "key", true })
			if err != nil {
				t.Fatal(err)
			}
			event, err := adapter.Submit(t.Context(), gw.GenerationCall{Prepared: prepared.Prepared})
			if (err != nil) != tc.invalid || !tc.invalid && (event.Observation.State != tc.state || event.Observation.ErrorCode != tc.code) {
				t.Fatalf("event %+v error %v", event, err)
			}
		})
	}
}

func TestRunningHubNanoBanana21Capabilities(t *testing.T) {
	policy := runningHubPolicy(t, "nano-banana-2.1")
	for _, tc := range []struct {
		name, parameters, mime string
		count, bytes           int
		allow                  bool
	}{
		{"defaults", `{}`, "image/png", 0, 0, true},
		{"wide_4k", `{"aspect_ratio":"8:1","resolution":"4K"}`, "image/jpeg", 9, 1, true},
		{"reference_limit", `{}`, "image/png", 1, 10 << 20, true},
		{"reference_too_large", `{}`, "image/png", 1, (10 << 20) + 1, false},
		{"too_many_references", `{}`, "image/png", 10, 1, false},
		{"unsupported_webp", `{}`, "image/webp", 1, 1, false},
		{"unsupported_quality", `{"quality":"high"}`, "image/png", 0, 0, false},
		{"unsupported_thinking", `{"thinking_level":"high"}`, "image/png", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refs := imageReferences{}
			req := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "nano-banana-2.1", Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "portrait", Parameters: json.RawMessage(tc.parameters)}}
			for i := range tc.count {
				id := fmt.Sprint(i)
				refs[id] = make([]byte, tc.bytes)
				req.Input.References = append(req.Input.References, refs.ref(id, tc.mime, gw.GenerationImage))
			}
			facts, err := gw.ReadGenerationFacts(t.Context(), req, refs)
			if err != nil {
				t.Fatal(err)
			}
			result, err := policy.Policy.Prepare(req, facts)
			allowed := err == nil && result.Verdict == gw.GenerationAllow
			if allowed != tc.allow {
				t.Fatalf("allow=%v want %v: %+v, %v", allowed, tc.allow, result, err)
			}
			if allowed && (result.Prepared.PolicyDefinition().Billing != gw.GenerationBillingDisabled) {
				t.Fatal("new model must retain unbilled policy")
			}
		})
	}
}

func TestRunningHubUploadResponseValidation(t *testing.T) {
	const validURL = "https://rh-hk-images-switch.xiaoyaoyou.com/reference.png"
	for _, tc := range []struct {
		name, code, location, kind string
		status                     int
		allow                      bool
	}{
		{"actual_success", `"code":0,`, validURL, "image", 200, true},
		{"legacy_host", `"code":0,`, "https://rh-images.xiaoyaoyou.com/ref.png", "image", 200, true},
		{"missing_code", "", validURL, "image", 200, false},
		{"null_code", `"code":null,`, validURL, "image", 200, false},
		{"wrong_code", `"code":200,`, validURL, "image", 200, false},
		{"error_code", `"code":500,`, validURL, "image", 200, false},
		{"http_error", `"code":0,`, validURL, "image", 500, false},
		{"redirect", `"code":0,`, validURL, "image", 302, false},
		{"wrong_type", `"code":0,`, validURL, "video", 200, false},
		{"insecure", `"code":0,`, "http://rh-hk-images-switch.xiaoyaoyou.com/ref.png", "image", 200, false},
		{"suffix_attack", `"code":0,`, "https://rh-hk-images-switch.xiaoyaoyou.com.evil.invalid/ref.png", "image", 200, false},
		{"userinfo", `"code":0,`, "https://evil@rh-hk-images-switch.xiaoyaoyou.com/ref.png", "image", 200, false},
		{"port", `"code":0,`, "https://rh-hk-images-switch.xiaoyaoyou.com:8443/ref.png", "image", 200, false},
		{"fragment", `"code":0,`, validURL + "#fragment", "image", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			submits, uploads := 0, 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/openapi/v2/media/upload/binary" {
					uploads++
					w.Header().Set("Location", "/must-not-follow")
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprintf(w, `{%s"data":{"type":%q,"download_url":%q}}`, tc.code, tc.kind, tc.location)
					return
				}
				submits++
				if r.URL.Path != "/openapi/v2/rhart-image-n-nb21/image-to-image" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var body struct {
					URLs []string `json:"imageUrls"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.URLs) != 1 || body.URLs[0] != tc.location {
					t.Errorf("reference mapping: %+v %v", body, err)
				}
				_, _ = io.WriteString(w, `{"taskId":"task","status":"QUEUED"}`)
			}))
			defer server.Close()
			refs := imageReferences{"ref": []byte("pixels")}
			req := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "nano-banana-2.1", Kind: gw.GenerationImage, Mode: "image", Input: gw.GenerationInput{Prompt: "p", References: []gw.GenerationReference{refs.ref("ref", "image/png", gw.GenerationImage)}, Parameters: json.RawMessage(`{}`)}}
			facts, err := gw.ReadGenerationFacts(t.Context(), req, refs)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := runningHubPolicy(t, req.ModelKey).Policy.Prepare(req, facts)
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := gw.NewRunningHubImages(server.Client(), server.URL, func() (string, bool) { return "key", true })
			if err != nil {
				t.Fatal(err)
			}
			event, err := adapter.Submit(t.Context(), gw.GenerationCall{Prepared: prepared.Prepared, References: refs})
			if tc.allow {
				if err != nil || event.Observation.TaskID != "task" || submits != 1 || uploads != 1 {
					t.Fatalf("valid upload: %v, %+v, submits=%d uploads=%d", err, event, submits, uploads)
				}
			} else if !errors.Is(err, gw.ErrNotDispatched) || submits != 0 || uploads != 1 {
				t.Fatalf("invalid upload: %v, submits=%d uploads=%d", err, submits, uploads)
			}
		})
	}
}
