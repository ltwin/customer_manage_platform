package llmgateway_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func generationFixture(t *testing.T, kind gw.GenerationKind) (gw.GenerationRequest, *gw.GenerationProfile) {
	t.Helper()
	r := modeRequest(kind)
	mode := testMode("generate", kind, true, true, `{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","minimum":1,"maximum":4}}}`)
	mode.Output = func(in gw.GenerationInput) (gw.GenerationOutputPolicy, error) {
		count := struct {
			Count int `json:"count"`
		}{Count: 1}
		if err := json.Unmarshal(in.Parameters, &count); err != nil {
			return gw.GenerationOutputPolicy{}, err
		}
		formats := map[gw.GenerationKind]string{gw.GenerationText: "text/plain", gw.GenerationImage: "image/png", gw.GenerationVideo: "video/mp4", gw.GenerationAudio: "audio/mpeg"}
		return gw.GenerationOutputPolicy{Kind: kind, MinOutputs: count.Count, MaxOutputs: count.Count, MIMEs: []string{formats[kind]}, MaxBytes: 1 << 20}, nil
	}
	return r, modeFixture(t, "model_a", mode)
}
func preparedGeneration(t *testing.T, r gw.GenerationRequest, p *gw.GenerationProfile) gw.PreparedGeneration {
	t.Helper()
	out, err := p.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGenerationRequestsAndFrozenIdentity(t *testing.T) {
	for _, kind := range []gw.GenerationKind{gw.GenerationText, gw.GenerationImage, gw.GenerationVideo, gw.GenerationAudio} {
		t.Run(string(kind), func(t *testing.T) {
			r, p := generationFixture(t, kind)
			first := preparedGeneration(t, r, p)
			explicit := r
			explicit.Mode = "generate"
			if preparedGeneration(t, explicit, p).Fingerprint() != first.Fingerprint() {
				t.Fatal("explicit and uniquely inferred mode differ")
			}
			r.Input.Prompt += "，胶片风格"
			if preparedGeneration(t, r, p).Fingerprint() == first.Fingerprint() {
				t.Fatal("prompt was not bound")
			}
			snapshot := first.Snapshot()
			snapshot.ProfileVersion = "profile-2"
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gw.RestoreGeneration(raw, first.Fingerprint()); !errors.Is(err, gw.ErrConflict) {
				t.Fatalf("changed profile version accepted: %v", err)
			}
		})
	}
}

func TestGenerationEnvelopeOnlyEnforcesCommonBounds(t *testing.T) {
	cases := map[string]func(*gw.GenerationRequest){
		"unknown kind":  func(r *gw.GenerationRequest) { r.Kind = "other" },
		"invalid utf8":  func(r *gw.GenerationRequest) { r.Input.Prompt = string([]byte{255}) },
		"wrong version": func(r *gw.GenerationRequest) { r.Version = "other" },
		"missing role":  func(r *gw.GenerationRequest) { r.Input.References = []gw.GenerationReference{pinnedReference("")} },
		"missing digest": func(r *gw.GenerationRequest) {
			r.Input.References = []gw.GenerationReference{pinnedReference("ref")}
			r.Input.References[0].Digest = ""
		},
		"bad mime kind": func(r *gw.GenerationRequest) {
			r.Input.References = []gw.GenerationReference{pinnedReference("ref")}
			r.Input.References[0].MIME = "video/mp4"
		},
		"negative byte size": func(r *gw.GenerationRequest) {
			r.Input.References = []gw.GenerationReference{pinnedReference("ref")}
			r.Input.References[0].ByteSize = -1
		},
		"excess prompt": func(r *gw.GenerationRequest) { r.Input.Prompt = strings.Repeat("a", (1<<20)+1) },
		"excess refs": func(r *gw.GenerationRequest) {
			for i := 0; i < 65; i++ {
				r.Input.References = append(r.Input.References, pinnedReference("ref"))
			}
		},
		"non-object parameters": func(r *gw.GenerationRequest) { r.Input.Parameters = json.RawMessage(`[]`) },
		"duplicate parameters":  func(r *gw.GenerationRequest) { r.Input.Parameters = json.RawMessage(`{"count":1,"count":2}`) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r, p := generationFixture(t, gw.GenerationImage)
			change(&r)
			if _, err := p.Prepare(r); !errors.Is(err, gw.ErrValidation) {
				t.Fatalf("want validation error, got %v", err)
			}
		})
	}
	r := modeRequest(gw.GenerationImage)
	r.Input.Prompt = ""
	r.Input.Parameters = json.RawMessage(`{"modelSpecificFlag":true}`)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.DecodeGenerationRequest(raw); err != nil {
		t.Fatalf("common envelope imposed model rules: %v", err)
	}
}

func TestGenerationReferenceIdentityAndOrder(t *testing.T) {
	r, p := generationFixture(t, gw.GenerationImage)
	r.Input.References = []gw.GenerationReference{pinnedReference("reference_image"), pinnedReference("style_image")}
	first := preparedGeneration(t, r, p).Fingerprint()
	r.Input.References[0], r.Input.References[1] = r.Input.References[1], r.Input.References[0]
	if preparedGeneration(t, r, p).Fingerprint() == first {
		t.Fatal("reference order was not bound")
	}
	r.Input.References[0].Digest = strings.Repeat("b", 64)
	changed := preparedGeneration(t, r, p).Fingerprint()
	r.Input.References[0].Role = "first_frame"
	if preparedGeneration(t, r, p).Fingerprint() == changed {
		t.Fatal("reference role was not bound")
	}
	r.Input.References = nil
	empty := preparedGeneration(t, r, p).Fingerprint()
	r.Input.References = []gw.GenerationReference{}
	if preparedGeneration(t, r, p).Fingerprint() != empty {
		t.Fatal("empty reference representations differ")
	}
}

func TestGenerationStrictJSONAndUnicode(t *testing.T) {
	r := modeRequest(gw.GenerationImage)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	original := string(raw)
	cases := map[string]string{
		"duplicate root":    strings.Replace(original, `"model_key":"model_a"`, `"model_key":"other","model_key":"model_a"`, 1),
		"root casing":       strings.Replace(original, `"model_key"`, `"Model_Key"`, 1),
		"input casing":      strings.Replace(original, `"prompt"`, `"Prompt"`, 1),
		"unknown field":     strings.Replace(original, `"parameters":{}`, `"parameters":{},"account_id":"forged"`, 1),
		"null field":        strings.Replace(original, `"parameters":{}`, `"parameters":null`, 1),
		"duplicate nested":  strings.Replace(original, `"parameters":{}`, `"parameters":{"count":1,"count":2}`, 1),
		"escaped duplicate": strings.Replace(original, `"parameters":{}`, `"parameters":{"count":1,"\u0063ount":2}`, 1),
		"damaged utf8":      strings.Replace(original, "逆光人像", string([]byte{255}), 1),
		"lone high":         strings.Replace(original, "逆光人像", `\ud800`, 1),
		"lone low":          strings.Replace(original, "逆光人像", `\udc00`, 1),
		"high then text":    strings.Replace(original, "逆光人像", `\ud800x`, 1),
		"two highs":         strings.Replace(original, "逆光人像", `\ud800\ud800`, 1),
		"multiple values":   original + original,
		"null root":         "null",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := gw.DecodeGenerationRequest([]byte(input)); !errors.Is(err, gw.ErrValidation) {
				t.Fatalf("ambiguous or damaged input accepted: %v", err)
			}
		})
	}
	valid := map[string]string{`\ud83d\ude00`: "😀", `\\ud800`: `\ud800`, `\/`: "/", `\ufffd`: "�"}
	for escaped, want := range valid {
		in := strings.Replace(original, "逆光人像", escaped, 1)
		out, err := gw.DecodeGenerationRequest([]byte(in))
		if err != nil || out.Input.Prompt != want {
			t.Fatalf("valid unicode changed: %v", err)
		}
	}
}

func TestGenerationMaximumSnapshotRoundTrip(t *testing.T) {
	for _, prompt := range []string{strings.Repeat("<", 1<<20), strings.Repeat("\x00", (1<<20)-1) + "x", strings.Repeat("😀", (1<<20)/4)} {
		r, p := generationFixture(t, gw.GenerationImage)
		r.Input.Prompt = prompt
		for i := 0; i < 64; i++ {
			ref := pinnedReference(strings.Repeat("r", 256))
			ref.RevisionID = strings.Repeat("i", 256)
			r.Input.References = append(r.Input.References, ref)
		}
		prepared := preparedGeneration(t, r, p)
		raw, err := json.Marshal(prepared.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		restored, err := gw.RestoreGeneration(raw, prepared.Fingerprint())
		if err != nil {
			t.Fatalf("maximum snapshot rejected (%d bytes): %v", len(raw), err)
		}
		if restored.Snapshot().Request.Input.Prompt != prompt {
			t.Fatal("snapshot changed prompt")
		}
	}
}
