package llmgateway_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// TestLiveAICodeWithImages 是GPT Image接入的真实证据：同一个Adapter提交、轮询、
// 取回结果，用量按冻结规则结算且不超过预留上界。每次全量约 0.15 元。
func TestLiveAICodeWithImages(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY"))
	if key == "" {
		t.Skip("AICODEWITH_API_KEY not set: skipping the real provider acceptance")
	}
	adapter, err := gw.NewAICodeWithImages(gw.NewHTTPClient(2*time.Minute), "https://api.aicodewith.ai", key)
	if err != nil {
		t.Fatal(err)
	}
	refs := imageReferences{"halves": liveGradientPNG(400, 400)}
	cases := []struct {
		name, model, params string
		list                []gw.GenerationReference
		prompt              string
		want                int
	}{
		{"image to image", "gpt-image-2.5-flare", `{"resolution":"1K","quality":"low"}`,
			[]gw.GenerationReference{refs.ref("halves", "image/png", gw.GenerationImage)},
			"Paint a ceramic mug whose left half and right half use exactly the two colors of the reference image.", 1},
		{"two transparent stickers", "gpt-image-2", `{"resolution":"1K","quality":"low","count":2,"background":"transparent"}`, nil,
			"A cartoon sticker of a cat, isolated on nothing.", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			request := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: c.model, Kind: gw.GenerationImage, Mode: gw.AICodeWithImageMode,
				Input: gw.GenerationInput{Prompt: c.prompt, References: c.list, Parameters: []byte(c.params)}}
			facts, err := gw.ReadGenerationFacts(t.Context(), request, refs)
			if err != nil {
				t.Fatal(err)
			}
			result, err := aicodewithPolicy(t, c.model).Policy.Prepare(request, facts)
			if err != nil || result.Verdict != gw.GenerationAllow {
				t.Fatalf("prepare %+v %v", result, err)
			}
			call := gw.GenerationCall{Prepared: result.Prepared, References: refs}
			event, err := adapter.Submit(t.Context(), call)
			if err != nil || event.Observation.State != gw.GenerationAccepted {
				t.Fatalf("submit %+v %v", event, err)
			}
			call.TaskID, call.References = event.Observation.TaskID, nil
			deadline := time.Now().Add(5 * time.Minute)
			for event.Observation.State == gw.GenerationAccepted || event.Observation.State == gw.GenerationRunning {
				if time.Now().After(deadline) {
					t.Fatal("the relay did not finish in time")
				}
				time.Sleep(5 * time.Second)
				if event, err = adapter.Observe(t.Context(), call); err != nil {
					t.Fatal(err)
				}
			}
			if err := event.Observation.Validate(result.Prepared.Generation()); err != nil || event.Observation.State != gw.GenerationCompleted || len(event.Observation.Outputs) != c.want {
				t.Fatalf("finished %+v %v", event, err)
			}
			price, err := result.Prepared.PriceUsage(event.Usage)
			if err != nil || price.TotalMicros > *result.Prepared.Quote().UpperBoundMicros {
				t.Fatalf("settlement %+v %v (usage %v)", price, err, event.Usage)
			}
			if c.list != nil && event.Usage["image_input_tokens"] == 0 {
				t.Fatal("the reference image was not sent")
			}
			for _, out := range event.Observation.Outputs {
				resp, err := http.Get(out.Media.FetchURL)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := io.ReadAll(io.LimitReader(resp.Body, out.Media.ByteSize+1))
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(raw, []byte("\x89PNG")) || int64(len(raw)) > out.Media.ByteSize {
					t.Fatalf("result %s: %d, %d bytes", out.ID, resp.StatusCode, len(raw))
				}
			}
			t.Logf("%s: usage %v, settled %d micros USD", c.model, event.Usage, price.TotalMicros)
		})
	}
}
