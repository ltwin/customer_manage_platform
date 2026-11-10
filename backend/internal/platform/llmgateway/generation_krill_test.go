package llmgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func krillPrepared(t *testing.T, parameters string, refs ...GenerationReference) PreparedPolicyGeneration {
	t.Helper()
	policies, err := KrillImagePolicies("USD")
	if err != nil {
		t.Fatal(err)
	}
	request := GenerationRequest{Version: GenerationContractVersion, ModelKey: "gpt-image-2.6-flare", Kind: GenerationImage, Mode: "image", Input: GenerationInput{Prompt: "blue square", Parameters: json.RawMessage(parameters), References: refs}}
	facts, err := ReadGenerationFacts(t.Context(), request, krillReferences{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := policies[1].Policy.Prepare(request, facts)
	if err != nil || result.Verdict != GenerationAllow {
		t.Fatalf("prepare: %+v %v", result, err)
	}
	return result.Prepared
}

type krillReferences struct{ corrupt bool }

func (r krillReferences) OpenGenerationReference(context.Context, GenerationReference) (io.ReadCloser, error) {
	data := "png"
	if r.corrupt {
		data = "bad"
	}
	return io.NopCloser(bytes.NewBufferString(data)), nil
}
func (krillReferences) ResolveGenerationMedia(_ context.Context, r GenerationReference) (GenerationMediaMetadata, error) {
	return GenerationMediaMetadata{RevisionID: r.RevisionID, Digest: r.Digest, ExtractorVersion: "test", Kind: r.Kind, MIME: r.MIME, ByteSize: r.ByteSize}, nil
}

func TestKrillImageProtocol(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(map[bool]string{false: "generation", true: "edit"}[edit], func(t *testing.T) {
			var refs []GenerationReference
			if edit {
				sum := sha256.Sum256([]byte("png"))
				refs = []GenerationReference{{Role: "reference", RevisionID: "ref", Kind: GenerationImage, MIME: "image/png", ByteSize: 3, Digest: hex.EncodeToString(sum[:])}}
			}
			prepared := krillPrepared(t, `{"aspect_ratio":"16:9","resolution":"2K","quality":"max"}`, refs...)
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing credential")
				}
				switch r.URL.Path {
				case "/v1/images/generations":
					if edit {
						t.Error("edit used generation endpoint")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["size"] != "2048x1152" || body["quality"] != "max" || body["async"] != true || body["n"] != float64(1) || body["output_format"] != "png" || body["model"] != "gpt-image-2.6-flare" {
						t.Errorf("body: %v", body)
					}
					if body["reasoning_effort"] != nil || body["resolution"] != nil {
						t.Error("unverified field sent")
					}
					_, _ = io.WriteString(w, `{"id":"img_1","status":"pending"}`)
				case "/v1/images/edits":
					if !edit {
						t.Error("generation used edits")
					}
					if err := r.ParseMultipartForm(1024); err != nil {
						t.Fatal(err)
					}
					defer func() { _ = r.MultipartForm.RemoveAll() }()
					if r.FormValue("size") != "2048x1152" || r.FormValue("async") != "true" || r.FormValue("n") != "1" || r.FormValue("quality") != "max" {
						t.Error("multipart fields")
					}
					f, _, err := r.FormFile("image[]")
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = f.Close() }()
					b, _ := io.ReadAll(f)
					if string(b) != "png" {
						t.Error("reference bytes")
					}
					_, _ = io.WriteString(w, `{"id":"img_1","status":"pending"}`)
				case "/v1/images/img_1":
					_, _ = io.WriteString(w, `{"id":"img_1","status":"completed","model":"gpt-image-2.6-flare","content_url":"/v1/images/img_1/content","expires_at":"2099-01-01T00:00:00Z"}`)
				default:
					t.Errorf("path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			a, err := NewKrillImages(server.Client(), server.URL, "test-key")
			if err != nil {
				t.Fatal(err)
			}
			call := GenerationCall{Prepared: prepared, References: krillReferences{}}
			event, err := a.Submit(t.Context(), call)
			if err != nil || event.Observation.State != GenerationAccepted {
				t.Fatalf("submit: %+v %v", event, err)
			}
			call.TaskID = event.Observation.TaskID
			event, err = a.Observe(t.Context(), call)
			if err != nil || event.Observation.State != GenerationCompleted || len(event.Observation.Outputs) != 1 || event.Usage != nil {
				t.Fatalf("observe: %+v %v", event, err)
			}
			if err := event.Observation.Validate(prepared.Generation()); err != nil {
				t.Fatal(err)
			}
			if edit {
				call.References = krillReferences{corrupt: true}
				if _, err := a.Submit(t.Context(), call); err == nil {
					t.Fatal("tampered reference sent")
				}
				if calls != 2 {
					t.Fatal("tampered bytes dispatched")
				}
			}
		})
	}
}

func TestKrillImageSizes(t *testing.T) {
	for _, tc := range []struct{ ratio, res, want string }{{"auto", "4K", "auto"}, {"1:1", "1K", "1024x1024"}, {"16:9", "1K", "1088x608"}, {"16:9", "2K", "2048x1152"}, {"16:9", "4K", "3840x2160"}, {"1:1", "4K", "2880x2880"}} {
		got, err := krillImageSize(tc.ratio, tc.res)
		if err != nil || got != tc.want {
			t.Errorf("%s/%s=%s %v want %s", tc.ratio, tc.res, got, err, tc.want)
		}
	}
}

func TestKrillImagePolicy(t *testing.T) {
	policies, err := KrillImagePolicies("USD")
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 {
		t.Fatal("model count")
	}
	for _, p := range policies {
		if p.Definition.Billing != GenerationBillingDisabled || p.Target.Billing != GenerationBillingDisabled || len(p.Definition.Modes[0].Pricing) != 0 {
			t.Fatal("unverified billing enabled")
		}
	}
	for _, params := range []string{`{"count":2}`, `{"quality":"ultra"}`, `{"reasoning_effort":"high"}`} {
		r := GenerationRequest{Version: GenerationContractVersion, ModelKey: policies[0].Target.ModelKey, Kind: GenerationImage, Mode: "image", Input: GenerationInput{Prompt: "square", Parameters: json.RawMessage(params)}}
		f, err := ReadGenerationFacts(t.Context(), r, krillReferences{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := policies[0].Policy.Prepare(r, f)
		if err == nil && result.Verdict == GenerationAllow {
			t.Errorf("accepted %s", params)
		}
	}
}

type krillTransport func(*http.Request) (*http.Response, error)

func (f krillTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKrillFetchCredentialBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, redirect string
		wantCalls      int
		ok             bool
	}{
		{"signed result", "https://" + krillImageFilesHost + "/krill/relay-image-inputs/test.png?signature=test", 2, true},
		{"foreign host", "https://evil.example/image.png", 1, false},
		{"wrong bucket", "https://" + krillImageFilesHost + "/other/test.png", 1, false},
		{"http", "http://" + krillImageFilesHost + "/krill/relay-image-inputs/test.png", 1, false},
		{"userinfo", "https://secret@" + krillImageFilesHost + "/krill/relay-image-inputs/test.png", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: krillTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				header := http.Header{}
				if calls == 1 {
					if r.URL.Host != "api.example" || r.Header.Get("Authorization") != "Bearer key" {
						t.Error("API credential missing")
					}
					header.Set("Location", tc.redirect)
					return &http.Response{StatusCode: 302, Header: header, Body: io.NopCloser(bytes.NewBufferString("")), Request: r}, nil
				}
				if r.Header.Get("Authorization") != "" {
					t.Fatal("credential leaked to image storage")
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(bytes.NewBufferString("png")), Request: r}, nil
			})}
			a, err := NewKrillImages(client, "https://api.example", "key")
			if err != nil {
				t.Fatal(err)
			}
			body, err := a.Open(t.Context(), "https://api.example/v1/images/img_1/content")
			if body != nil {
				_ = body.Close()
			}
			if (err == nil) != tc.ok || calls != tc.wantCalls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			for _, bad := range []string{"https://evil.example/v1/images/img_1/content", "https://api.example/v1/images/img_1/content?x=1", "https://api.example/v1/images/../content", "https://api.example/v1/images/img_a/b/content", "https://api.example/v1/images/img_1%2fcontent"} {
				if _, err := a.Open(t.Context(), bad); err == nil {
					t.Errorf("allowed %s", bad)
				}
			}
			if calls != tc.wantCalls {
				t.Fatal("unsafe content location dispatched")
			}
		})
	}
}

func TestKrillImageUnknownAndTerminalStates(t *testing.T) {
	prepared := krillPrepared(t, `{}`)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   GenerationObservationState
	}{
		{"pending", 200, `{"id":"img_1","status":"pending","model":"gpt-image-2.6-flare"}`, GenerationRunning},
		{"failed", 200, `{"id":"img_1","status":"failed","model":"gpt-image-2.6-flare","error":"private prompt"}`, GenerationFailed},
		{"missing", 404, `{}`, GenerationUnknown},
		{"server error", 500, `{}`, ""},
		{"wrong identity", 200, `{"id":"img_other","status":"failed","model":"gpt-image-2.6-flare"}`, ""},
		{"wrong model", 200, `{"id":"img_1","status":"failed","model":"other"}`, ""},
		{"unknown", 200, `{"id":"img_1","status":"unexpected","model":"gpt-image-2.6-flare"}`, ""},
		{"foreign result", 200, `{"id":"img_1","status":"completed","model":"gpt-image-2.6-flare","content_url":"https://evil.example/x"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			a, err := NewKrillImages(server.Client(), server.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			e, err := a.Observe(t.Context(), GenerationCall{TaskID: "img_1", Prepared: prepared})
			if tc.want == "" {
				if err == nil {
					t.Fatal("accepted invalid observation")
				}
				return
			}
			if err != nil || e.Observation.State != tc.want || e.Usage != nil {
				t.Fatalf("%+v %v", e, err)
			}
		})
	}
}
