package llmgateway_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// 测试模型刻意使用不同模式规则，不代表任何真实供应商能力。
func modeFixture(t *testing.T, model string, modes ...gw.GenerationMode) *gw.GenerationProfile {
	t.Helper()
	p, err := gw.NewGenerationProfile(gw.GenerationProfileDefinition{ModelKey: model, ModelRevision: "revision-1", Version: "profile-1"}, modes)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func modeSchema(requiredPrompt bool, refs bool, parameters string) json.RawMessage {
	promptRequired := ""
	if requiredPrompt {
		promptRequired = `"required":["prompt"],`
	}
	referenceProperty := ""
	if refs {
		referenceProperty = `,"references":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"role":{"type":"string"},"revision_id":{"type":"string"},"digest":{"type":"string"},"kind":{"type":"string"},"mime":{"type":"string"},"byte_size":{"type":"integer"}}}}`
	}
	return json.RawMessage(`{"type":"object","additionalProperties":false,` + promptRequired + `"properties":{"prompt":{"type":"string","minLength":1},"parameters":` + parameters + referenceProperty + `}}`)
}
func testMode(key string, kind gw.GenerationKind, requiredPrompt, refs bool, parameters string) gw.GenerationMode {
	mime := string(kind) + "/test"
	if kind == gw.GenerationText {
		mime = "text/plain"
	}
	return gw.GenerationMode{Definition: gw.GenerationModeDefinition{Key: key, Version: "mode-1", Kind: kind, InputSchema: modeSchema(requiredPrompt, refs, parameters)},
		Output: func(gw.GenerationInput) (gw.GenerationOutputPolicy, error) {
			return gw.GenerationOutputPolicy{Kind: kind, MinOutputs: 1, MaxOutputs: 1, MIMEs: []string{mime}, MaxBytes: 1 << 20}, nil
		}}
}

const sizeParams = `{"type":"object","additionalProperties":false,"properties":{"size":{"type":"integer","enum":[512,1024]}}}`

func modeRequest(kind gw.GenerationKind) gw.GenerationRequest {
	return gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "model_a", Kind: kind, Input: gw.GenerationInput{Prompt: "逆光人像", Parameters: json.RawMessage(`{}`)}}
}
func pinnedReference(role string) gw.GenerationReference {
	return gw.GenerationReference{Role: role, RevisionID: "revision-1", Digest: strings.Repeat("a", 64), Kind: gw.GenerationImage, MIME: "image/png", ByteSize: 100}
}

func TestGenerationModelOwnsModesParametersAndDefaults(t *testing.T) {
	text := testMode("text_to_image", gw.GenerationImage, true, false, sizeParams)
	image := testMode("image_to_image", gw.GenerationImage, false, true, sizeParams)
	image.Normalize = func(in gw.GenerationInput) (json.RawMessage, error) {
		if len(in.References) != 1 || in.References[0].Role != "reference_image" {
			return nil, gw.ErrCapability
		}
		return json.RawMessage(`{"size":512}`), nil
	}
	p := modeFixture(t, "model_a", text, image)
	r := modeRequest(gw.GenerationImage)
	a, err := p.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Request.Mode != "text_to_image" {
		t.Fatal("wrong inferred text mode")
	}
	r.Input.Prompt = ""
	r.Input.References = []gw.GenerationReference{pinnedReference("reference_image")}
	b, err := p.Prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().Request.Mode != "image_to_image" || string(b.Snapshot().Request.Input.Parameters) != `{"size":512}` {
		t.Fatal("mode did not own prompt rule or defaults")
	}
	r.Mode = "text_to_image"
	if _, err := p.Prepare(r); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("incompatible explicit mode accepted: %v", err)
	}
	r = modeRequest(gw.GenerationImage)
	r.Input.Parameters = json.RawMessage(`{"size":768}`)
	if _, err := p.Prepare(r); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("unsupported model parameter accepted: %v", err)
	}
	other := testMode("text_to_image", gw.GenerationImage, true, false, `{"type":"object","additionalProperties":false,"properties":{"size":{"type":"integer","enum":[768]}}}`)
	q := modeFixture(t, "model_b", other)
	r.ModelKey = "model_b"
	if _, err := q.Prepare(r); err != nil {
		t.Fatalf("model inherited another model's rules: %v", err)
	}
	if _, err := p.Prepare(r); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("profile accepted another model: %v", err)
	}
}

func TestGenerationRolesAndAmbiguousModes(t *testing.T) {
	frames := testMode("first_last_frame", gw.GenerationVideo, false, true, `{"type":"object","additionalProperties":false,"properties":{}}`)
	frames.Normalize = func(in gw.GenerationInput) (json.RawMessage, error) {
		seen := map[string]int{}
		for _, r := range in.References {
			seen[r.Role]++
		}
		if len(in.References) != 2 || seen["first_frame"] != 1 || seen["last_frame"] != 1 {
			return nil, gw.ErrCapability
		}
		return in.Parameters, nil
	}
	p := modeFixture(t, "model_a", frames)
	r := modeRequest(gw.GenerationVideo)
	r.Input.Prompt = ""
	r.Input.References = []gw.GenerationReference{pinnedReference("first_frame"), pinnedReference("last_frame")}
	if _, err := p.Prepare(r); err != nil {
		t.Fatal(err)
	}
	r.Input.References[1].Role = "first_frame"
	if _, err := p.Prepare(r); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("duplicate frame role accepted: %v", err)
	}
	a := testMode("mode_a", gw.GenerationImage, true, false, sizeParams)
	b := testMode("mode_b", gw.GenerationImage, true, false, sizeParams)
	p = modeFixture(t, "model_a", a, b)
	r = modeRequest(gw.GenerationImage)
	if _, err := p.Prepare(r); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("ambiguous mode silently selected: %v", err)
	}
	r.Mode = "mode_b"
	if _, err := p.Prepare(r); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationFrozenSnapshotDoesNotRerunModelRules(t *testing.T) {
	mode := testMode("text_to_image", gw.GenerationImage, true, false, sizeParams)
	calls := 0
	mode.Normalize = func(in gw.GenerationInput) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"size":512}`), nil
	}
	p := modeFixture(t, "model_a", mode)
	prepared, err := p.Prepare(modeRequest(gw.GenerationImage))
	if err != nil {
		t.Fatal(err)
	}
	snap := prepared.Snapshot()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := gw.RestoreGeneration(raw, prepared.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || restored.Fingerprint() != prepared.Fingerprint() {
		t.Fatal("restore reran rules or changed identity")
	}
	snap.Request.Input.Parameters[0] = 'x'
	snap.Output.MIMEs[0] = "image/other"
	snap.InputSchema[0] = 'x'
	if _, err := gw.RestoreGeneration(raw, prepared.Fingerprint()); err != nil {
		t.Fatalf("snapshot mutation escaped: %v", err)
	}
	bad := strings.Replace(string(raw), `"mode":"text_to_image"`, `"mode":"image_to_image"`, 1)
	if _, err := gw.RestoreGeneration([]byte(bad), prepared.Fingerprint()); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("mode mutation accepted: %v", err)
	}
	mode.Definition.InputSchema[0] = 'x'
	if _, err := p.Prepare(modeRequest(gw.GenerationImage)); err != nil {
		t.Fatalf("registration retained caller schema slice: %v", err)
	}
}

func TestGenerationModelSchemaRejectsUnsupportedAndExactNumericInputs(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		input  string
	}{
		{"fraction hidden by float rounding", `{"type":"integer"}`, `1.00000000000000001`},
		{"large integer above maximum", `{"type":"integer","maximum":9007199254740992}`, `9007199254740993`},
		{"large integer outside enum", `{"type":"integer","enum":[9007199254740992]}`, `9007199254740993`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode := testMode("generate", gw.GenerationImage, true, false, `{"type":"object","additionalProperties":false,"properties":{"seed":`+tc.schema+`}}`)
			p := modeFixture(t, "model_a", mode)
			r := modeRequest(gw.GenerationImage)
			r.Input.Parameters = json.RawMessage(`{"seed":` + tc.input + `}`)
			if _, err := p.Prepare(r); !errors.Is(err, gw.ErrCapability) {
				t.Fatalf("numeric schema silently rounded input: %v", err)
			}
		})
	}
}

func TestGenerationModelRegistrationAndNormalizerBoundary(t *testing.T) {
	mode := testMode("generate", gw.GenerationImage, true, false, sizeParams)
	for _, schema := range []string{
		`{"type":"object","additionalProperties":false,"$ref":"https://example.test/schema"}`,
		`{"type":"object","additionalProperties":false,"unknownConstraint":true}`,
		`{"type":"object","additionalProperties":false,"properties":{"prompt":{"type":"string","format":"unknown-format"}}}`,
	} {
		bad := mode
		bad.Definition.InputSchema = json.RawMessage(schema)
		if _, err := gw.NewGenerationProfile(gw.GenerationProfileDefinition{ModelKey: "model_a", ModelRevision: "r1", Version: "v1"}, []gw.GenerationMode{bad}); !errors.Is(err, gw.ErrValidation) {
			t.Fatalf("unimplemented schema accepted: %v", err)
		}
	}
	mode.Normalize = func(gw.GenerationInput) (json.RawMessage, error) { return json.RawMessage(`{"size":768}`), nil }
	p := modeFixture(t, "model_a", mode)
	if _, err := p.Prepare(modeRequest(gw.GenerationImage)); !errors.Is(err, gw.ErrProtocol) {
		t.Fatalf("normalizer bypassed schema: %v", err)
	}
	broken := testMode("broken", gw.GenerationImage, true, false, sizeParams)
	broken.Normalize = func(gw.GenerationInput) (json.RawMessage, error) { return nil, errors.New("secret provider details") }
	p = modeFixture(t, "model_a", broken, testMode("fallback", gw.GenerationImage, true, false, sizeParams))
	if _, err := p.Prepare(modeRequest(gw.GenerationImage)); !errors.Is(err, gw.ErrProtocol) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("normalizer failure fell back or leaked: %v", err)
	}
}

func TestGenerationModeControlsOutputRangeForAnyModality(t *testing.T) {
	mode := testMode("multi_video", gw.GenerationVideo, false, false, `{"type":"object","additionalProperties":false,"properties":{}}`)
	mode.Output = func(gw.GenerationInput) (gw.GenerationOutputPolicy, error) {
		return gw.GenerationOutputPolicy{Kind: gw.GenerationVideo, MinOutputs: 1, MaxOutputs: 2, MIMEs: []string{"video/mp4"}, MaxBytes: 1000}, nil
	}
	r := modeRequest(gw.GenerationVideo)
	r.Input.Prompt = ""
	prepared := preparedGeneration(t, r, modeFixture(t, "model_a", mode))
	o := completedGeneration(r)
	o.Outputs = append(o.Outputs, o.Outputs[0])
	o.Outputs[1].ID = "output-2"
	o.Outputs[1].Index = 1
	if err := o.Validate(prepared); err != nil {
		t.Fatalf("common observer imposed single-video restriction: %v", err)
	}
	o.Outputs = append(o.Outputs, o.Outputs[0])
	o.Outputs[2].ID = "output-3"
	o.Outputs[2].Index = 2
	if err := o.Validate(prepared); !errors.Is(err, gw.ErrProtocol) {
		t.Fatalf("mode output limit ignored: %v", err)
	}
}

func TestGenerationSchemaAdmissionRejectsUnenforcedConstraints(t *testing.T) {
	definitions := map[string]string{
		"uuid format":               `{"type":"string","format":"uuid"}`,
		"email format":              `{"type":"string","format":"email"}`,
		"ipv4 format":               `{"type":"string","format":"ipv4"}`,
		"uri format":                `{"type":"string","format":"uri"}`,
		"empty enum":                `{"type":"integer","enum":[]}`,
		"overflow minLength":        `{"type":"string","minLength":9223372036854775808}`,
		"overflow maxLength":        `{"type":"string","maxLength":9223372036854775808}`,
		"overflow minItems":         `{"type":"array","items":{"type":"string"},"minItems":9223372036854775808}`,
		"overflow maxItems":         `{"type":"array","items":{"type":"string"},"maxItems":9223372036854775808}`,
		"reversed numeric bounds":   `{"type":"integer","minimum":2,"maximum":1}`,
		"duplicate equivalent enum": `{"type":"integer","enum":[1,1.0]}`,
	}
	for name, node := range definitions {
		t.Run(name, func(t *testing.T) {
			m := testMode("generate", gw.GenerationImage, true, false, `{"type":"object","additionalProperties":false,"properties":{"value":`+node+`}}`)
			if _, err := gw.NewGenerationProfile(gw.GenerationProfileDefinition{ModelKey: "model_a", ModelRevision: "r1", Version: "v1"}, []gw.GenerationMode{m}); !errors.Is(err, gw.ErrValidation) {
				t.Fatalf("unenforced schema constraint admitted: %v", err)
			}
		})
	}
}

func TestGenerationCompositeEnumsUseExactRecursiveEquality(t *testing.T) {
	cases := []struct{ name, schema, good, bad string }{
		{"array", `{"type":"array","items":{"type":"integer"},"enum":[[512,512],[1024,1024]]}`, `[512,512]`, `[512,1024]`},
		{"object", `{"type":"object","additionalProperties":false,"properties":{"width":{"type":"integer"}},"enum":[{"width":512}]}`, `{"width":512}`, `{"width":1024}`},
		{"nested large integer", `{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"seed":{"type":"integer"}}},"enum":[[{"seed":9007199254740993}]]}`, `[{"seed":9007199254740993}]`, `[{"seed":9007199254740992}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testMode("generate", gw.GenerationImage, true, false, `{"type":"object","additionalProperties":false,"properties":{"value":`+tc.schema+`}}`)
			p := modeFixture(t, "model_a", m)
			r := modeRequest(gw.GenerationImage)
			r.Input.Parameters = json.RawMessage(`{"value":` + tc.good + `}`)
			prepared, err := p.Prepare(r)
			if err != nil {
				t.Fatalf("valid composite enum rejected: %v", err)
			}
			raw, err := json.Marshal(prepared.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gw.RestoreGeneration(raw, prepared.Fingerprint()); err != nil {
				t.Fatalf("composite enum did not restore: %v", err)
			}
			r.Input.Parameters = json.RawMessage(`{"value":` + tc.bad + `}`)
			if _, err := p.Prepare(r); !errors.Is(err, gw.ErrCapability) {
				t.Fatalf("invalid composite enum accepted: %v", err)
			}
		})
	}
}
