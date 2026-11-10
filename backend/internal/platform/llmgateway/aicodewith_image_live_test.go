package llmgateway_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// liveLinks 把参考身份映射到中转能取到的公网地址：用前一个用例的结果图作参考。
type liveLinks struct {
	imageReferences
	urls map[string]string
}

func (l liveLinks) LinkGenerationReference(_ context.Context, ref gw.GenerationReference) (string, error) {
	return l.urls[ref.RevisionID], nil
}

// TestLiveAICodeWithImages 是GPT Image接入的真实证据：同一个Adapter提交、轮询、
// 取回结果，按张结算且不超过预留上界。图生图用前一个用例的结果地址作参考。
// 每次全量4张1K低画质。
func TestLiveAICodeWithImages(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY"))
	if key == "" {
		t.Skip("AICODEWITH_API_KEY not set: skipping the real provider acceptance")
	}
	adapter, err := gw.NewAICodeWithImages(gw.NewHTTPClient(2*time.Minute), "https://api.aicodewith.ai", key)
	if err != nil {
		t.Fatal(err)
	}
	// 摘要事实只用于准入；中转按地址取图，地址在前一用例完成后填入。
	refs := liveLinks{imageReferences: imageReferences{"previous": liveGradientPNG(400, 400)}, urls: map[string]string{}}
	previous := []gw.GenerationReference{refs.ref("previous", "image/png", gw.GenerationImage)}
	cases := []struct {
		name, model, params string
		list                []gw.GenerationReference
		prompt              string
	}{
		{"2.6 sunburst", "gpt-image-2.6-sunburst", `{"resolution":"1K","quality":"low"}`, nil,
			"A paper boat on a calm lake at dawn."},
		{"2.6 flare image to image", "gpt-image-2.6-flare", `{"resolution":"1K","quality":"low"}`, previous,
			"Keep the composition, restyle the reference image as a paper cutout."},
		{"2.5 flare image to image", "gpt-image-2.5-flare", `{"resolution":"1K"}`, previous,
			"Keep the composition, restyle the reference image as a watercolor."},
		{"transparent sticker", "gpt-image-2", `{"resolution":"1K","quality":"low","background":"transparent"}`, nil,
			"A cartoon sticker of a cat, isolated on nothing."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.list != nil && refs.urls["previous"] == "" {
				t.Skip("no earlier result to use as the reference")
			}
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
			if err := event.Observation.Validate(result.Prepared.Generation()); err != nil || event.Observation.State != gw.GenerationCompleted || len(event.Observation.Outputs) != 1 {
				t.Fatalf("finished %+v %v", event, err)
			}
			price, err := result.Prepared.PriceUsage(event.Usage)
			if err != nil || price.TotalMicros > *result.Prepared.Quote().UpperBoundMicros {
				t.Fatalf("settlement %+v %v (usage %v)", price, err, event.Usage)
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
			refs.urls["previous"] = event.Observation.Outputs[0].Media.FetchURL
			t.Logf("%s: usage %v, settled %d micros USD", c.model, event.Usage, price.TotalMicros)
		})
	}
}
