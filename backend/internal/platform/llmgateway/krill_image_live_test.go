package llmgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"io"
	"os"
	"testing"
	"time"
)

type krillLiveReference struct{ data []byte }

func (r krillLiveReference) OpenGenerationReference(context.Context, GenerationReference) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}
func (r krillLiveReference) ResolveGenerationMedia(ctx context.Context, ref GenerationReference) (GenerationMediaMetadata, error) {
	return (krillReferences{}).ResolveGenerationMedia(ctx, ref)
}

// TestLiveKrillImages 仅显式启用时消费真实额度；两次提交覆盖两个模型和参考图编辑。
func TestLiveKrillImages(t *testing.T) {
	if os.Getenv("KRILL_AI_IMAGE_LIVE") != "1" {
		t.Skip("set KRILL_AI_IMAGE_LIVE=1 to enable paid image checks")
	}
	key := os.Getenv("KRILL_AI_API_KEY")
	if key == "" {
		t.Fatal("KRILL_AI_API_KEY required")
	}
	a, err := NewKrillImages(NewHTTPClient(2*time.Minute), "https://api.kbsfdxc.net", key)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := KrillImagePolicies("USD")
	if err != nil {
		t.Fatal(err)
	}
	parameters := os.Getenv("KRILL_AI_IMAGE_LIVE_PARAMETERS")
	if parameters == "" {
		parameters = `{"resolution":"1K","quality":"low"}`
	}
	var previous []byte
	for _, p := range policies {
		t.Run(p.Target.ModelKey, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
			defer cancel()
			r := GenerationRequest{Version: GenerationContractVersion, ModelKey: p.Target.ModelKey, Kind: GenerationImage, Mode: "image", Input: GenerationInput{Prompt: "A small blue square centered on a plain white background.", Parameters: json.RawMessage(parameters)}}
			refs := krillLiveReference{data: previous}
			if len(previous) > 0 {
				sum := sha256.Sum256(previous)
				r.Input.References = []GenerationReference{{Role: "reference", RevisionID: "live_reference", Kind: GenerationImage, MIME: "image/png", ByteSize: int64(len(previous)), Digest: hex.EncodeToString(sum[:])}}
				r.Input.Prompt = "Change the blue square to green, keeping the white background."
			}
			facts, err := ReadGenerationFacts(ctx, r, refs)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := p.Policy.Prepare(r, facts)
			if err != nil || prepared.Verdict != GenerationAllow {
				t.Fatalf("prepare: %+v %v", prepared, err)
			}
			call := GenerationCall{Prepared: prepared.Prepared, References: refs}
			event, err := a.Submit(ctx, call)
			if err != nil {
				t.Fatal(err)
			}
			if event.Observation.State != GenerationAccepted {
				t.Fatalf("submit state %s", event.Observation.State)
			}
			call.TaskID = event.Observation.TaskID
			for {
				event, err = a.Observe(ctx, call)
				if err != nil {
					t.Fatal(err)
				}
				if event.Observation.State == GenerationCompleted {
					break
				}
				if event.Observation.State != GenerationRunning {
					t.Fatalf("terminal state %s", event.Observation.State)
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Second):
				}
			}
			if err := event.Observation.Validate(prepared.Prepared.Generation()); err != nil {
				t.Fatal(err)
			}
			body, err := a.Open(ctx, event.Observation.Outputs[0].Media.FetchURL)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(io.LimitReader(body, aicodewithImageMaxBytes+1))
			_ = body.Close()
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil || len(data) > aicodewithImageMaxBytes {
				t.Fatalf("invalid PNG: %v", err)
			}
			t.Logf("completed; %dx%d PNG, %d bytes, references=%d", cfg.Width, cfg.Height, len(data), len(r.Input.References))
			previous = data
		})
	}
}
