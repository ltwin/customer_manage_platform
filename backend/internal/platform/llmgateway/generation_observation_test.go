package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func completedGeneration(r gw.GenerationRequest) gw.GenerationObservation {
	out := gw.GenerationOutput{ID: "output-1", Kind: r.Kind}
	switch r.Kind {
	case gw.GenerationText:
		out.Text = "生成的作品"
	case gw.GenerationImage:
		out.Media = &gw.GeneratedMedia{MIME: "image/png", ByteSize: 100, FetchURL: "https://results.example.test/output?signature=secret", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	case gw.GenerationVideo:
		out.Media = &gw.GeneratedMedia{MIME: "video/mp4", ByteSize: 100, FetchURL: "https://results.example.test/video", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	case gw.GenerationAudio:
		out.Media = &gw.GeneratedMedia{MIME: "audio/mpeg", ByteSize: 100, FetchURL: "https://results.example.test/audio", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	}
	return gw.GenerationObservation{State: gw.GenerationCompleted, Outputs: []gw.GenerationOutput{out}}
}

// 两个受控适配器仅模拟协议差异，不冒充生产Gateway派发或数据库恢复。
type controlledSyncGeneration struct{}

func (controlledSyncGeneration) Submit(_ context.Context, r gw.GenerationRequest) gw.GenerationObservation {
	return completedGeneration(r)
}

type controlledAsyncGeneration struct {
	task  string
	calls int
}

func (a *controlledAsyncGeneration) Submit(_ context.Context, _ gw.GenerationRequest) gw.GenerationObservation {
	a.calls++
	return gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: a.task, RetryAfter: time.Second}
}
func (a *controlledAsyncGeneration) Observe(_ context.Context, task string, r gw.GenerationRequest) gw.GenerationObservation {
	if task != a.task {
		return gw.GenerationObservation{State: gw.GenerationUnknown, ErrorCode: "task_not_found"}
	}
	out := completedGeneration(r)
	out.TaskID = task
	return out
}

func TestGenerationSyncAndAsyncObservationContract(t *testing.T) {
	for _, kind := range []gw.GenerationKind{gw.GenerationText, gw.GenerationImage, gw.GenerationVideo, gw.GenerationAudio} {
		t.Run(string(kind), func(t *testing.T) {
			r, p := generationFixture(t, kind)
			sync := controlledSyncGeneration{}.Submit(context.Background(), r)
			if err := sync.Validate(preparedGeneration(t, r, p)); err != nil {
				t.Fatal(err)
			}
			async := &controlledAsyncGeneration{task: "provider-task-1"}
			accepted := async.Submit(context.Background(), r)
			if err := accepted.Validate(preparedGeneration(t, r, p)); err != nil {
				t.Fatal(err)
			}
			if len(accepted.Outputs) != 0 {
				t.Fatal("accepted task exposed a completed output")
			}
			completed := async.Observe(context.Background(), accepted.TaskID, r)
			if err := completed.Validate(preparedGeneration(t, r, p)); err != nil {
				t.Fatal(err)
			}
			if async.calls != 1 || sync.Outputs[0].ID != completed.Outputs[0].ID {
				t.Fatal("adapter protocols diverged")
			}
		})
	}
}

func TestGenerationObservationRejectsProtocolViolations(t *testing.T) {
	cases := []struct {
		name   string
		change func(*gw.GenerationObservation, *gw.GenerationRequest, *gw.GenerationProfile)
	}{
		{"unknown state", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.State = "surprise"
		}},
		{"completed without outputs", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) { o.Outputs = nil }},
		{"too few outputs", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			r.Input.Parameters = json.RawMessage(`{"count":2}`)
		}},
		{"duplicate output id", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			r.Input.Parameters = json.RawMessage(`{"count":2}`)
			o.Outputs = append(o.Outputs, o.Outputs[0])
			o.Outputs[1].Index = 1
		}},
		{"unordered outputs", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Index = 1
		}},
		{"changed modality", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Kind = gw.GenerationAudio
		}},
		{"mixed result payload", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Text = "text"
		}},
		{"changed mime", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Media.MIME = "image/jpeg"
		}},
		{"oversized output", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Media.ByteSize = (1 << 20) + 1
		}},
		{"unknown size", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Media.ByteSize = 0
		}},
		{"insecure url", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Media.FetchURL = "http://results.example.test/output"
		}},
		{"credential url", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Media.FetchURL = "https://key:secret@results.example.test/output"
		}},
		{"missing expiry", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.Outputs[0].Media.ExpiresAt = time.Time{}
		}},
		{"nan progress", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			v := math.NaN()
			o.Progress = &v
		}},
		{"partial completion progress", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			v := 0.5
			o.Progress = &v
		}},
		{"completion with error", func(o *gw.GenerationObservation, r *gw.GenerationRequest, p *gw.GenerationProfile) {
			o.ErrorCode = "failed"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, p := generationFixture(t, gw.GenerationImage)
			o := completedGeneration(r)
			tc.change(&o, &r, p)
			if err := o.Validate(preparedGeneration(t, r, p)); !errors.Is(err, gw.ErrProtocol) {
				t.Fatalf("want protocol error, got %v", err)
			}
		})
	}
}

func TestGenerationPendingAndFailureEvidence(t *testing.T) {
	cases := []struct {
		name  string
		obs   gw.GenerationObservation
		valid bool
	}{
		{"accepted", gw.GenerationObservation{State: gw.GenerationAccepted, TaskID: "task-1"}, true},
		{"running", gw.GenerationObservation{State: gw.GenerationRunning, TaskID: "task-1"}, true},
		{"accepted without task", gw.GenerationObservation{State: gw.GenerationAccepted}, false},
		{"running without task", gw.GenerationObservation{State: gw.GenerationRunning}, false},
		{"unknown without task", gw.GenerationObservation{State: gw.GenerationUnknown, ErrorCode: "response_lost"}, true},
		{"unknown with task", gw.GenerationObservation{State: gw.GenerationUnknown, TaskID: "task-1", ErrorCode: "query_timeout"}, true},
		{"rejected", gw.GenerationObservation{State: gw.GenerationRejected, ErrorCode: "invalid_request"}, true},
		{"rejected after acceptance", gw.GenerationObservation{State: gw.GenerationRejected, TaskID: "task-1", ErrorCode: "invalid_request"}, false},
		{"failed accepted task", gw.GenerationObservation{State: gw.GenerationFailed, TaskID: "task-1", ErrorCode: "generation_failed"}, true},
		{"failed synchronous generation", gw.GenerationObservation{State: gw.GenerationFailed, ErrorCode: "generation_failed"}, true},
		{"cancelled accepted task", gw.GenerationObservation{State: gw.GenerationCancelled, TaskID: "task-1", ErrorCode: "cancelled"}, true},
		{"unbounded retry delay", gw.GenerationObservation{State: gw.GenerationRunning, TaskID: "task-1", RetryAfter: time.Hour}, false},
		{"negative retry delay", gw.GenerationObservation{State: gw.GenerationRunning, TaskID: "task-1", RetryAfter: -time.Second}, false},
		{"raw error detail", gw.GenerationObservation{State: gw.GenerationUnknown, ErrorCode: "https://secret.example.test/?key=secret"}, false},
	}
	r, p := generationFixture(t, gw.GenerationImage)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obs.Validate(preparedGeneration(t, r, p))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v got %v", tc.valid, err)
			}
			tc.obs.Outputs = completedGeneration(r).Outputs
			if err := tc.obs.Validate(preparedGeneration(t, r, p)); !errors.Is(err, gw.ErrProtocol) {
				t.Fatalf("non-completed state accepted outputs: %v", err)
			}
		})
	}
}

func TestGenerationLocatorStaysOutOfJSON(t *testing.T) {
	r, p := generationFixture(t, gw.GenerationImage)
	o := completedGeneration(r)
	o.TaskID = "private-task"
	if err := o.Validate(preparedGeneration(t, r, p)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private-task") || strings.Contains(string(raw), "example.test") {
		t.Fatalf("protected location leaked: %s", raw)
	}
	// 普通JSON投影不能当作可恢复快照；缺少保护字段的结果必须拒绝。
	var projected gw.GenerationObservation
	if err := json.Unmarshal(raw, &projected); err != nil {
		t.Fatal(err)
	}
	if err := projected.Validate(preparedGeneration(t, r, p)); !errors.Is(err, gw.ErrProtocol) {
		t.Fatalf("lossy projection accepted for recovery: %v", err)
	}
}
