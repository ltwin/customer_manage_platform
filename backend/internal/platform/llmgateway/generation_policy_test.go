package llmgateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func policyFixture(t *testing.T) (gw.GenerationPolicyDefinition, gw.GenerationPolicyTarget) {
	t.Helper()
	mode := testMode("reference_video", gw.GenerationVideo, false, true, `{"type":"object","additionalProperties":false,"required":["duration"],"properties":{"duration":{"type":"integer","minimum":1,"maximum":10000},"quality":{"type":"string","enum":["standard","high"]}}}`)
	output, err := mode.Output(gw.GenerationInput{})
	if err != nil {
		t.Fatal(err)
	}
	aggregate := func(op, field, unit string) gw.GenerationValueRef {
		return gw.GenerationValueRef{Source: "references", Aggregate: op, Field: field, Kind: gw.GenerationVideo, Role: "reference_video", Unit: unit}
	}
	condition := func(ref gw.GenerationValueRef, op string, value string) gw.GenerationCondition {
		return gw.GenerationCondition{Op: op, Ref: ref, Values: []json.RawMessage{json.RawMessage(value)}}
	}
	d := gw.GenerationPolicyDefinition{EngineVersion: gw.GenerationPolicyEngineVersion, Version: "policy-1", Profile: gw.GenerationProfileDefinition{ModelKey: "model_a", ModelRevision: "revision-1", Version: "profile-1"}, DeploymentID: "deployment-1", AdapterVersion: "adapter-1", Currency: "CNY", Modes: []gw.GenerationPolicyMode{{Definition: mode.Definition, Output: output, Defaults: map[string]json.RawMessage{"duration": json.RawMessage(`5000`)}, Parameters: map[string]gw.GenerationFactType{"duration": {Type: "number", Unit: "millisecond"}, "quality": {Type: "string"}}, Rules: []gw.GenerationConstraint{
		{ID: "count", Assert: condition(aggregate("count", "", "count"), "lte", "3")},
		{ID: "each_duration", Assert: condition(aggregate("each", "duration_ms", "millisecond"), "lte", "15000")},
		{ID: "total_duration", Assert: condition(aggregate("sum", "duration_ms", "millisecond"), "lte", "20000")},
	}, Pricing: []gw.GenerationPriceComponent{{ID: "video", Estimate: gw.GenerationValueRef{Source: "parameter", Field: "duration", Unit: "millisecond"}, Usage: "output_ms", Unit: "millisecond", Divisor: 1000, QuantityRound: "ceil", Tiers: []gw.GenerationPriceTier{{ID: "base", Fallback: true, Rate: gw.GenerationRatio{Numerator: 125000, Denominator: 1}}}}}}}}
	target := gw.GenerationPolicyTarget{ModelKey: "model_a", ModelRevision: "revision-1", DeploymentID: "deployment-1", AdapterVersion: "adapter-1", Modes: []string{"reference_video"}, Usage: map[string]gw.GenerationUsageDefinition{"output_ms": {Unit: "millisecond", UpperBound: newInt64(10000)}}}
	return d, target
}
func compilePolicy(t *testing.T, d gw.GenerationPolicyDefinition, target gw.GenerationPolicyTarget) *gw.CompiledGenerationPolicy {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	p, err := gw.CompileGenerationPolicy(raw, target)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type policyMediaReader struct {
	duration *int64
	calls    int
}

func (r *policyMediaReader) ResolveGenerationMedia(_ context.Context, ref gw.GenerationReference) (gw.GenerationMediaMetadata, error) {
	r.calls++
	return gw.GenerationMediaMetadata{RevisionID: ref.RevisionID, Digest: ref.Digest, ExtractorVersion: "probe-1", Kind: ref.Kind, MIME: ref.MIME, ByteSize: ref.ByteSize, DurationMS: r.duration}, nil
}
func policyRequest() gw.GenerationRequest {
	r := modeRequest(gw.GenerationVideo)
	r.Mode = "reference_video"
	ref := pinnedReference("reference_video")
	ref.Kind = gw.GenerationVideo
	ref.MIME = "video/mp4"
	r.Input.References = []gw.GenerationReference{ref, ref}
	return r
}
func TestGenerationPolicyReferenceLimitsAndTrustedFacts(t *testing.T) {
	d, target := policyFixture(t)
	p := compilePolicy(t, d, target)
	for _, tc := range []struct {
		name     string
		duration *int64
		verdict  gw.GenerationVerdict
	}{
		{"total exceeds", newInt64(12000), gw.GenerationDeny}, {"boundary", newInt64(10000), gw.GenerationAllow}, {"unknown", nil, gw.GenerationNeedsFacts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := policyRequest()
			reader := &policyMediaReader{duration: tc.duration}
			facts, err := gw.ReadGenerationFacts(context.Background(), r, reader)
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.Prepare(r, facts)
			if err != nil {
				t.Fatal(err)
			}
			if result.Verdict != tc.verdict {
				t.Fatalf("verdict: %s; issues: %+v", result.Verdict, result.Issues)
			}
			if reader.calls != 1 {
				t.Fatalf("duplicate content probed %d times", reader.calls)
			}
			if tc.verdict != gw.GenerationAllow && result.Prepared.Fingerprint() != "" {
				t.Fatal("non-allow produced prepared request")
			}
			if tc.verdict == gw.GenerationDeny && result.Issues[0].RuleID != "total_duration" {
				t.Fatalf("wrong violation: %+v", result.Issues)
			}
		})
	}
}
func newInt64(v int64) *int64 { return &v }
func TestGenerationPolicyQuoteFreezeAndActualUsage(t *testing.T) {
	d, target := policyFixture(t)
	p := compilePolicy(t, d, target)
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("prepare: %+v %v", result, err)
	}
	q := result.Prepared.Quote()
	if q.EstimateMicros != 625000 || q.UpperBoundMicros == nil || *q.UpperBoundMicros != 1250000 {
		t.Fatalf("quote: %+v", q)
	}
	raw, err := result.Prepared.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := gw.RestorePolicyGeneration(raw, result.Prepared.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	settlement, err := restored.PriceUsage(map[string]int64{"output_ms": 6001})
	if err != nil || settlement.TotalMicros != 875000 {
		t.Fatalf("settlement: %+v %v", settlement, err)
	}
	if _, err := restored.PriceUsage(nil); !errors.Is(err, gw.ErrGenerationFactsUnavailable) {
		t.Fatalf("missing usage: %v", err)
	}
	if _, err := restored.PriceUsage(map[string]int64{"output_ms": 10001}); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("bound exceeded: %v", err)
	}
	d.Modes[0].Pricing[0].Tiers[0].Rate.Numerator = 999999
	_ = compilePolicy(t, d, target)
	again, err := restored.PriceUsage(map[string]int64{"output_ms": 6001})
	if err != nil || again.TotalMicros != 875000 {
		t.Fatalf("old price changed: %+v %v", again, err)
	}
}

func TestGenerationPolicyCompileRejectsUnsupportedOrAmbiguousConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*gw.GenerationPolicyDefinition, *gw.GenerationPolicyTarget)
	}{
		{"unknown engine", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) { d.EngineVersion = "unknown" }},
		{"adapter binding", func(_ *gw.GenerationPolicyDefinition, b *gw.GenerationPolicyTarget) { b.AdapterVersion = "other" }},
		{"unsupported mode", func(_ *gw.GenerationPolicyDefinition, b *gw.GenerationPolicyTarget) { b.Modes = []string{"other"} }},
		{"duplicate rule", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) { d.Modes[0].Rules[1].ID = "count" }},
		{"unknown operator", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Rules[0].Assert.Op = "exec"
		}},
		{"wrong unit", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Rules[1].Assert.Ref.Unit = "second"
		}},
		{"missing parameter", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Pricing[0].Estimate.Field = "missing"
		}},
		{"type mismatch", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Parameters["duration"] = gw.GenerationFactType{Type: "string"}
		}},
		{"bad default", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Defaults["duration"] = json.RawMessage(`10001`)
		}},
		{"zero divisor", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Pricing[0].Divisor = 0
		}},
		{"zero denominator", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Pricing[0].Tiers[0].Rate.Denominator = 0
		}},
		{"missing fallback", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Pricing[0].Tiers = nil
		}},
		{"duplicate usage", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			c := d.Modes[0].Pricing[0]
			c.ID = "other"
			d.Modes[0].Pricing = append(d.Modes[0].Pricing, c)
		}},
		{"bound overflow", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Pricing[0].Tiers[0].Rate.Numerator = 9223372036854775807
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b := policyFixture(t)
			tc.mutate(&d, &b)
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gw.CompileGenerationPolicy(raw, b); err == nil {
				t.Fatal("invalid configuration compiled")
			}
		})
	}
	d, b := policyFixture(t)
	raw, _ := json.Marshal(d)
	for _, replacement := range []string{`"op":"lte","unexpected":1`, `"op":"lte","op":"gte"`, `"Op":"lte"`} {
		broken := strings.Replace(string(raw), `"op":"lte"`, replacement, 1)
		if _, err := gw.CompileGenerationPolicy([]byte(broken), b); err == nil {
			t.Fatalf("invalid JSON compiled: %s", replacement)
		}
	}
}

func TestGenerationPolicyExactPricingTiersModifiersAndUnknowns(t *testing.T) {
	d, b := policyFixture(t)
	price := &d.Modes[0].Pricing[0]
	price.Tiers[0].Rate = gw.GenerationRatio{Numerator: 1, Denominator: 3}
	price.QuantityRound = "exact"
	usageCondition := gw.GenerationCondition{Op: "gte", Ref: gw.GenerationValueRef{Source: "usage", Field: "output_ms", Unit: "millisecond"}, Values: []json.RawMessage{json.RawMessage(`6000`)}}
	price.Tiers = append(price.Tiers, gw.GenerationPriceTier{ID: "long", When: &usageCondition, Rate: gw.GenerationRatio{Numerator: 2, Denominator: 3}})
	price.Modifiers = []gw.GenerationPriceModifier{{ID: "boost", When: usageCondition, Factor: gw.GenerationRatio{Numerator: 3, Denominator: 2}}}
	p := compilePolicy(t, d, b)
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(r, facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Prepared.Quote().EstimateMicros != 2 {
		t.Fatalf("exact quote: %+v", result.Prepared.Quote())
	}
	actual, err := result.Prepared.PriceUsage(map[string]int64{"output_ms": 6001})
	if err != nil || actual.TotalMicros != 7 || actual.Lines[0].Tier != "long" || len(actual.Lines[0].Modifiers) != 1 {
		t.Fatalf("actual tier: %+v %v", actual, err)
	}
	// 同时命中多个条件价阶必须拒绝，不能依赖配置排列顺序。
	price.Tiers = append(price.Tiers, gw.GenerationPriceTier{ID: "duplicate_match", When: &usageCondition, Rate: gw.GenerationRatio{Numerator: 1, Denominator: 1}})
	p = compilePolicy(t, d, b)
	result, err = p.Prepare(r, facts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = result.Prepared.PriceUsage(map[string]int64{"output_ms": 6001}); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("ambiguous tiers: %v", err)
	}
	// 缺失计价条件事实时，即使有fallback也不得报价。
	price.Tiers = price.Tiers[:1]
	price.Modifiers[0].When = gw.GenerationCondition{Op: "gte", Ref: gw.GenerationValueRef{Source: "references", Kind: gw.GenerationVideo, Aggregate: "sum", Field: "duration_ms", Unit: "millisecond"}, Values: []json.RawMessage{json.RawMessage(`0`)}}
	d.Modes[0].Rules = nil
	p = compilePolicy(t, d, b)
	facts, err = gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{})
	if err != nil {
		t.Fatal(err)
	}
	result, err = p.Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationNeedsFacts {
		t.Fatalf("unknown price: %+v %v", result, err)
	}
}

func TestGenerationPolicyDefaultsModeSelectionAndCopies(t *testing.T) {
	d, b := policyFixture(t)
	p := compilePolicy(t, d, b)
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	r.Input.Parameters = json.RawMessage(`{"duration":3000}`)
	result, err := p.Prepare(r, facts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Prepared.Quote().EstimateMicros != 375000 {
		t.Fatal("default replaced explicit input")
	}
	view := p.Definition()
	view.Modes[0].Pricing[0].Tiers[0].Rate.Numerator = 0
	q := result.Prepared.Quote()
	*q.UpperBoundMicros = 0
	q.Lines[0].CostMicros = 0
	if result.Prepared.Quote().EstimateMicros != 375000 || *result.Prepared.Quote().UpperBoundMicros == 0 {
		t.Fatal("quote shared mutable state")
	}
	raw, err := result.Prepared.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), `"estimate_micros":375000`, `"estimate_micros":0`, 1)
	if _, err := gw.RestorePolicyGeneration([]byte(changed), result.Prepared.Fingerprint()); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("tampered quote: %v", err)
	}
	if _, err := gw.RestorePolicyGeneration(raw, strings.Repeat("0", 64)); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("tampered identity: %v", err)
	}
	// 参考身份、角色和顺序都参与事实绑定。
	r.Input.References[0].Role = "first_frame"
	if _, err := p.Prepare(r, facts); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("facts reused with different role: %v", err)
	}
	r = policyRequest()
	r.Mode = ""
	m := d.Modes[0]
	m.Definition.Key = "second"
	d.Modes = append(d.Modes, m)
	b.Modes = append(b.Modes, "second")
	p = compilePolicy(t, d, b)
	if _, err := p.Prepare(r, facts); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("ambiguous mode: %v", err)
	}
}

func TestGenerationPolicyCatalogAtomicPublication(t *testing.T) {
	d, b := policyFixture(t)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var catalog gw.GenerationPolicyCatalog
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	rev, err := catalog.Publish(raw, b, 0, now.Add(time.Hour), now)
	if err != nil || rev != 1 {
		t.Fatalf("publish: %d %v", rev, err)
	}
	old, _, err := catalog.Snapshot(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Publish([]byte(`{}`), b, 1, now.Add(time.Hour), now); err == nil {
		t.Fatal("bad configuration published")
	}
	current, currentRev, err := catalog.Snapshot(now)
	if err != nil || current != old || currentRev != 1 {
		t.Fatal("failed publication replaced active version")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidate := old.Definition()
			candidate.Version = fmt.Sprintf("policy-%d", i+2)
			encoded, e := json.Marshal(candidate)
			if e != nil {
				t.Error(e)
				return
			}
			_, e = catalog.Publish(encoded, b, 1, now.Add(time.Hour), now)
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, gw.ErrConflict) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("publication winners: %d", wins.Load())
	}
	if _, _, err := catalog.Snapshot(now.Add(time.Hour)); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("expired policy available: %v", err)
	}
	d.Modes[0].Pricing[0].Tiers[0].Rate.Numerator = 1
	raw, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Publish(raw, b, 2, now.Add(time.Hour), now); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("version reused: %v", err)
	}
	if old.Version() != "policy-1" {
		t.Fatal("old snapshot changed")
	}
}

func TestGenerationPolicyMissingBoundEmptyReferencesAndPrecision(t *testing.T) {
	d, b := policyFixture(t)
	b.Usage["output_ms"] = gw.GenerationUsageDefinition{Unit: "millisecond"}
	p := compilePolicy(t, d, b)
	r := policyRequest()
	r.Input.References = nil
	facts, err := gw.ReadGenerationFacts(context.Background(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("empty references: %+v %v", result, err)
	}
	if result.Prepared.Quote().UpperBoundMicros != nil {
		t.Fatal("invented upper bound")
	}
	d.Modes[0].Parameters["duration"] = gw.GenerationFactType{Type: "number", Unit: "millisecond"}
	d.Modes[0].Definition.InputSchema = modeSchema(false, true, `{"type":"object","additionalProperties":false,"properties":{"duration":{"type":"integer"},"quality":{"type":"string"}}}`)
	d.Modes[0].Pricing[0].Divisor = 1
	d.Modes[0].Pricing[0].Tiers[0].Rate = gw.GenerationRatio{Numerator: 1, Denominator: 1}
	p = compilePolicy(t, d, b)
	r.Input.Parameters = json.RawMessage(`{"duration":9007199254740993}`)
	result, err = p.Prepare(r, facts)
	if err != nil || result.Prepared.Quote().EstimateMicros != 9007199254740993 {
		t.Fatalf("precision lost: %+v %v", result.Prepared.Quote(), err)
	}
}

func TestGenerationPolicyConditionsAndUnknownMode(t *testing.T) {
	d, b := policyFixture(t)
	exists := gw.GenerationCondition{Op: "exists", Ref: gw.GenerationValueRef{Source: "parameter", Field: "quality"}}
	quality := gw.GenerationCondition{Op: "in", Ref: gw.GenerationValueRef{Source: "parameter", Field: "quality"}, Values: []json.RawMessage{json.RawMessage(`"high"`)}}
	rule := gw.GenerationConstraint{ID: "conditional_quality", When: &exists, Assert: gw.GenerationCondition{Op: "all", Children: []gw.GenerationCondition{quality, {Op: "not", Children: []gw.GenerationCondition{{Op: "eq", Ref: quality.Ref, Values: []json.RawMessage{json.RawMessage(`"standard"`)}}}}}}}
	d.Modes[0].Rules = append(d.Modes[0].Rules, rule)
	p := compilePolicy(t, d, b)
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		params string
		want   gw.GenerationVerdict
	}{{`{}`, gw.GenerationAllow}, {`{"quality":"high"}`, gw.GenerationAllow}, {`{"quality":"standard"}`, gw.GenerationDeny}} {
		r.Input.Parameters = json.RawMessage(tc.params)
		res, err := p.Prepare(r, facts)
		if err != nil || res.Verdict != tc.want {
			t.Fatalf("conditional rule %s: %+v %v", tc.params, res, err)
		}
	}
	// 一个候选不需要时长，另一个候选因时长未知尚不能排除，不得自动选择前者。
	second := d.Modes[0]
	second.Definition.Key = "unconstrained"
	second.Rules = nil
	d.Modes = append(d.Modes, second)
	b.Modes = append(b.Modes, "unconstrained")
	p = compilePolicy(t, d, b)
	r = policyRequest()
	r.Mode = ""
	facts, err = gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Prepare(r, facts)
	if err != nil || res.Verdict != gw.GenerationNeedsFacts {
		t.Fatalf("unknown candidate ignored: %+v %v", res, err)
	}
}

func TestGenerationPolicyIssuesDoNotMutateRules(t *testing.T) {
	d, b := policyFixture(t)
	p := compilePolicy(t, d, b)
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(12000)})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Prepare(r, facts)
	if err != nil || res.Verdict != gw.GenerationDeny {
		t.Fatal("missing initial rejection")
	}
	for _, issue := range res.Issues {
		for _, obs := range issue.Observations {
			for _, value := range obs.Expected {
				for i := range value {
					value[i] = '9'
				}
			}
		}
	}
	res, err = p.Prepare(r, facts)
	if err != nil || res.Verdict != gw.GenerationDeny {
		t.Fatalf("diagnostic mutated policy: %+v %v", res, err)
	}
}

func TestGenerationPolicyMissingPriceNumeratorIsNotFree(t *testing.T) {
	d, b := policyFixture(t)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"numerator":125000,`, "", 1))
	if _, err := gw.CompileGenerationPolicy(raw, b); !errors.Is(err, gw.ErrValidation) {
		t.Fatalf("missing price numerator accepted: %v", err)
	}
}

type generationPolicyReaderFunc func(context.Context, gw.GenerationReference) (gw.GenerationMediaMetadata, error)

func (f generationPolicyReaderFunc) ResolveGenerationMedia(ctx context.Context, r gw.GenerationReference) (gw.GenerationMediaMetadata, error) {
	return f(ctx, r)
}

func TestGenerationPolicyTrustedMediaBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*gw.GenerationMediaMetadata)
	}{
		{"digest", func(m *gw.GenerationMediaMetadata) { m.Digest = strings.Repeat("b", 64) }},
		{"size", func(m *gw.GenerationMediaMetadata) { m.ByteSize++ }},
		{"mime", func(m *gw.GenerationMediaMetadata) { m.MIME = "video/webm" }},
		{"extractor", func(m *gw.GenerationMediaMetadata) { m.ExtractorVersion = "" }},
		{"negative duration", func(m *gw.GenerationMediaMetadata) { m.DurationMS = newInt64(-1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := generationPolicyReaderFunc(func(ctx context.Context, r gw.GenerationReference) (gw.GenerationMediaMetadata, error) {
				m, err := (&policyMediaReader{}).ResolveGenerationMedia(ctx, r)
				tc.mutate(&m)
				return m, err
			})
			if _, err := gw.ReadGenerationFacts(context.Background(), policyRequest(), reader); err == nil {
				t.Fatal("unverified media facts accepted")
			}
		})
	}
	r := policyRequest()
	if _, err := gw.ReadGenerationFacts(context.Background(), r, nil); !errors.Is(err, gw.ErrGenerationFactsUnavailable) {
		t.Fatalf("missing reader: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gw.ReadGenerationFacts(ctx, r, &policyMediaReader{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled probe continued: %v", err)
	}
	// Reader返回的指针后续变化不得修改已建立的事实。
	duration := int64(12000)
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: &duration})
	if err != nil {
		t.Fatal(err)
	}
	duration = 1000
	d, b := policyFixture(t)
	res, err := compilePolicy(t, d, b).Prepare(r, facts)
	if err != nil || res.Verdict != gw.GenerationDeny {
		t.Fatalf("reader mutated frozen facts: %+v %v", res, err)
	}
}

func TestGenerationPolicyExampleConfiguration(t *testing.T) {
	raw, err := os.ReadFile("../../../../deploy/generation-policies/example-video.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	target := gw.GenerationPolicyTarget{ModelKey: "example-video", ModelRevision: "revision-1", DeploymentID: "example-deployment", AdapterVersion: "adapter-1", Modes: []string{"reference_video"}, Usage: map[string]gw.GenerationUsageDefinition{"output_ms": {Unit: "millisecond", UpperBound: newInt64(10000)}}}
	policy, err := gw.CompileGenerationPolicy(raw, target)
	if err != nil {
		t.Fatal(err)
	}
	r := policyRequest()
	r.ModelKey = "example-video"
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(10000)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := policy.Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationAllow || result.Prepared.Quote().EstimateMicros != 625000 {
		t.Fatalf("example: %+v %v", result, err)
	}
}

func TestGenerationPolicyAggregateUnknownAndInapplicable(t *testing.T) {
	d, b := policyFixture(t)
	d.Modes[0].Rules = nil
	when := gw.GenerationCondition{Op: "gte", Ref: gw.GenerationValueRef{Source: "references", Aggregate: "sum", Field: "duration_ms", Unit: "millisecond"}, Values: []json.RawMessage{json.RawMessage(`1000`)}}
	d.Modes[0].Pricing[0].Tiers = append(d.Modes[0].Pricing[0].Tiers, gw.GenerationPriceTier{ID: "with_duration", When: &when, Rate: gw.GenerationRatio{Numerator: 250000, Denominator: 1}})
	p := compilePolicy(t, d, b)
	for _, reverse := range []bool{false, true} {
		r := policyRequest()
		r.Input.References = []gw.GenerationReference{pinnedReference("image"), r.Input.References[0]}
		r.Input.References[1].RevisionID = "video-1"
		if reverse {
			slices.Reverse(r.Input.References)
		}
		facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := p.Prepare(r, facts)
		if err != nil || result.Verdict != gw.GenerationNeedsFacts {
			t.Fatalf("unknown aggregation reverse=%v: %+v %v", reverse, result, err)
		}
		reader := generationPolicyReaderFunc(func(ctx context.Context, ref gw.GenerationReference) (gw.GenerationMediaMetadata, error) {
			m, e := (&policyMediaReader{}).ResolveGenerationMedia(ctx, ref)
			if ref.Kind == gw.GenerationVideo {
				m.DurationMS = newInt64(1000)
			}
			return m, e
		})
		facts, err = gw.ReadGenerationFacts(context.Background(), r, reader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Prepare(r, facts); !errors.Is(err, gw.ErrCapability) {
			t.Fatalf("inapplicable aggregation became fallback: %v", err)
		}
	}
	r := policyRequest()
	r.Input.References = []gw.GenerationReference{pinnedReference("image")}
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{})
	if err != nil {
		t.Fatal(err)
	}
	d.Modes[0].Pricing[0].Tiers = d.Modes[0].Pricing[0].Tiers[:1]
	each := gw.GenerationCondition{Op: "lte", Ref: gw.GenerationValueRef{Source: "references", Aggregate: "each", Field: "duration_ms", Unit: "millisecond"}, Values: []json.RawMessage{json.RawMessage(`1000`)}}
	d.Modes[0].Rules = []gw.GenerationConstraint{{ID: "not_duration", Assert: gw.GenerationCondition{Op: "not", Children: []gw.GenerationCondition{each}}}}
	if _, err := compilePolicy(t, d, b).Prepare(r, facts); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("inapplicable comparison negated into permission: %v", err)
	}
	// 只有显式exists可以检查不适用，不把它当作普通数值。
	d.Modes[0].Rules[0].Assert.Children[0].Op = "exists"
	d.Modes[0].Rules[0].Assert.Children[0].Values = nil
	result, err := compilePolicy(t, d, b).Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("explicit presence: %+v %v", result, err)
	}
}

func TestGenerationPolicyCanonicalSnapshotClosure(t *testing.T) {
	d, b := policyFixture(t)
	literal := strings.Repeat("<", 1000)
	d.Modes[0].Rules = nil
	encoded, err := json.Marshal(literal)
	if err != nil {
		t.Fatal(err)
	}
	when := gw.GenerationCondition{Op: "eq", Ref: gw.GenerationValueRef{Source: "prompt"}, Values: []json.RawMessage{encoded}}
	d.Modes[0].Pricing[0].Tiers = append(d.Modes[0].Pricing[0].Tiers, gw.GenerationPriceTier{ID: "html_literal", When: &when, Rate: gw.GenerationRatio{Numerator: 1, Denominator: 1}})
	// 使用未转义输入配置验证编译、持久表示与恢复采用相同规则。
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte(`\u003c`), []byte("<"))
	p, err := gw.CompileGenerationPolicy(raw, b)
	if err != nil {
		t.Fatal(err)
	}
	r := policyRequest()
	r.Input.Prompt = literal
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(r, facts)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := result.Prepared.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := gw.RestorePolicyGeneration(snapshot, result.Prepared.Fingerprint())
	if err != nil || restored.Quote().EstimateMicros != 5 {
		t.Fatalf("HTML literal round trip: %+v %v", restored.Quote(), err)
	}
	// 转义后越过整个规则包上限，必须在编译发布前拒绝。
	d.Modes[0].Pricing[0].Tiers = d.Modes[0].Pricing[0].Tiers[:1]
	for i := 0; i < 30; i++ {
		value, e := json.Marshal(strings.Repeat("<", 3000))
		if e != nil {
			t.Fatal(e)
		}
		cond := gw.GenerationCondition{Op: "eq", Ref: gw.GenerationValueRef{Source: "prompt"}, Values: []json.RawMessage{value}}
		d.Modes[0].Pricing[0].Tiers = append(d.Modes[0].Pricing[0].Tiers, gw.GenerationPriceTier{ID: fmt.Sprintf("tier_%d", i), When: &cond, Rate: gw.GenerationRatio{Numerator: 1, Denominator: 1}})
	}
	raw, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte(`\u003c`), []byte("<"))
	if _, err := gw.CompileGenerationPolicy(raw, b); !errors.Is(err, gw.ErrValidation) {
		t.Fatalf("canonical oversized policy compiled: %v", err)
	}
}

func TestGenerationPolicyAdapterUsageTypesAreAuthoritative(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*gw.GenerationPolicyDefinition, *gw.GenerationPolicyTarget)
	}{
		{"reinterpreted unit", func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
			d.Modes[0].Parameters["duration"] = gw.GenerationFactType{Type: "number", Unit: "count"}
			d.Modes[0].Pricing[0].Estimate.Unit = "count"
			d.Modes[0].Pricing[0].Unit = "count"
		}},
		{"unsupported usage", func(_ *gw.GenerationPolicyDefinition, b *gw.GenerationPolicyTarget) { delete(b.Usage, "output_ms") }},
		{"invalid declared unit", func(_ *gw.GenerationPolicyDefinition, b *gw.GenerationPolicyTarget) {
			b.Usage["output_ms"] = gw.GenerationUsageDefinition{Unit: "second"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b := policyFixture(t)
			tc.mutate(&d, &b)
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gw.CompileGenerationPolicy(raw, b); err == nil {
				t.Fatal("invalid adapter usage binding compiled")
			}
		})
	}
	d, b := policyFixture(t)
	p := compilePolicy(t, d, b)
	*b.Usage["output_ms"].UpperBound = 0
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(r, facts)
	if err != nil || *result.Prepared.Quote().UpperBoundMicros != 1250000 {
		t.Fatalf("target limit reference leaked: %+v %v", result, err)
	}
}

func TestGenerationPolicyExampleRejectsUndeclaredRoles(t *testing.T) {
	raw, err := os.ReadFile("../../../../deploy/generation-policies/example-video.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	target := gw.GenerationPolicyTarget{ModelKey: "example-video", ModelRevision: "revision-1", DeploymentID: "example-deployment", AdapterVersion: "adapter-1", Modes: []string{"reference_video"}, Usage: map[string]gw.GenerationUsageDefinition{"output_ms": {Unit: "millisecond", UpperBound: newInt64(10000)}}}
	policy, err := gw.CompileGenerationPolicy(raw, target)
	if err != nil {
		t.Fatal(err)
	}
	r := policyRequest()
	r.ModelKey = "example-video"
	r.Input.References[1].Role = "undeclared_role"
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: newInt64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := policy.Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationDeny {
		t.Fatalf("example accepted undeclared role: %+v %v", result, err)
	}
}
