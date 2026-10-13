package creativeimageedit

import (
	"bufio"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

// TestResourceSpike measures the full render and the Go verifier at
// 12/24/60 MP. It is a measurement, not a gate: run it on purpose with
// IMAGE_EDIT_SPIKE=1 in the worker toolchain (docs/dev/creative-image-edit.md).
//
// Every step runs in its own child process started by this small parent.
// Go starts children with vfork and Linux keeps the parent's RSS high-water
// mark across exec, so a parent holding the sample images would show up in
// every child's ru_maxrss.
func TestResourceSpike(t *testing.T) {
	if os.Getenv("IMAGE_EDIT_SPIKE") == "" {
		t.Skip("set IMAGE_EDIT_SPIKE=1 to measure")
	}
	newCodec(t)
	sizes := []struct {
		name string
		w, h int
	}{{"12MP", 4240, 2832}, {"24MP", 6000, 4000}, {"60MP", 9480, 6328}}
	t.Logf("| 图 | 动作 | 源 | 输出 | vips 耗时 | vips 峰值 RSS | 中间文件峰值 | Go 校验耗时 | Go 校验峰值 RSS |")
	t.Logf("|---|---|---|---|---|---|---|---|---|")
	for _, size := range sizes {
		dir := t.TempDir()
		spikeChild(t, "generate", dir, strconv.Itoa(size.w), strconv.Itoa(size.h))
		jpegPath, pngPath := filepath.Join(dir, "photo.jpg"), filepath.Join(dir, "cutout.png")
		cases := []struct {
			action string
			req    Request
		}{
			{"压缩 JPEG Q75", Request{Source: jpegPath, Mime: "image/jpeg", Action: ActionCompress, Quality: 75}},
			{"裁切 JPEG 中央 80%", Request{Source: jpegPath, Mime: "image/jpeg", Action: ActionCrop, Crop: Crop{Left: size.w / 10, Top: size.h / 10, Width: size.w * 8 / 10, Height: size.h * 8 / 10}}},
			{"透明 PNG 压缩 → WebP Q75", Request{Source: pngPath, Mime: "image/png", Action: ActionCompress, Quality: 75}},
		}
		for _, tc := range cases {
			req, _ := json.Marshal(tc.req)
			out, _, _ := spikeChild(t, "render", dir, string(req))
			var r spikeRender
			if err := json.Unmarshal([]byte(lastJSON(out)), &r); err != nil {
				t.Fatalf("%s %s: %v: %s", size.name, tc.action, err, out)
			}
			_, verifyElapsed, verifyRSS := spikeChild(t, "verify", dir, r.Output, r.Mime)
			source, _ := os.Stat(tc.req.Source)
			t.Logf("| %s | %s | %s | %s | %s | %s | %s | %s | %s |", size.name, tc.action, mib(source.Size()), mib(r.Size),
				time.Duration(r.Elapsed).Round(10*time.Millisecond), mib(r.PeakRSS), mib(r.PeakDisk), verifyElapsed.Round(10*time.Millisecond), mib(verifyRSS))
			_ = os.Remove(r.Output)
		}
	}
}

type spikeRender struct {
	Output   string
	Mime     string
	Size     int64
	Elapsed  int64
	PeakRSS  int64
	PeakDisk int64
}

// TestSpikeChild is the child side of the spike, selected by environment.
func TestSpikeChild(t *testing.T) {
	mode := os.Getenv("IMAGE_EDIT_SPIKE_CHILD")
	if mode == "" {
		t.Skip("spike child process only")
	}
	dir := os.Getenv("IMAGE_EDIT_SPIKE_DIR")
	args := strings.Split(os.Getenv("IMAGE_EDIT_SPIKE_ARGS"), "\x1f")
	switch mode {
	case "generate":
		w, _ := strconv.Atoi(args[0])
		h, _ := strconv.Atoi(args[1])
		writeImage(t, filepath.Join(dir, "photo.jpg"), func(f *os.File) error {
			return jpeg.Encode(f, photoLike(w, h, false), &jpeg.Options{Quality: 92})
		})
		writeImage(t, filepath.Join(dir, "cutout.png"), func(f *os.File) error {
			return (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, photoLike(w, h, true))
		})
	case "render":
		var req Request
		if err := json.Unmarshal([]byte(args[0]), &req); err != nil {
			t.Fatal(err)
		}
		result, err := newCodec(t).Render(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		kept := filepath.Join(dir, "output"+filepath.Ext(result.Path))
		if err := os.Rename(result.Path, kept); err != nil {
			t.Fatal(err)
		}
		_ = result.Close()
		out, _ := json.Marshal(spikeRender{Output: kept, Mime: result.Mime, Size: result.Size, Elapsed: int64(result.Elapsed), PeakRSS: result.peakRSS, PeakDisk: result.PeakDisk})
		_, _ = os.Stdout.Write(append(out, '\n'))
	case "verify":
		f, err := os.Open(args[0])
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		_, staged, err := creativemedia.NewVerifier(creativemedia.DefaultConfig()).Verify(t.Context(), bufio.NewReader(f), "image", args[1], 1<<31)
		if err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(staged)
	}
}

func spikeChild(t *testing.T, mode, dir string, args ...string) (string, time.Duration, int64) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpikeChild$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "IMAGE_EDIT_SPIKE_CHILD="+mode, "IMAGE_EDIT_SPIKE_DIR="+dir, "IMAGE_EDIT_SPIKE_ARGS="+strings.Join(args, "\x1f"))
	started := time.Now()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("spike %s: %v: %s", mode, err, out)
	}
	return string(out), time.Since(started), maxRSSBytes(cmd.ProcessState.SysUsage().(*syscall.Rusage))
}

func lastJSON(out string) string {
	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "{") {
			return line
		}
	}
	return ""
}

// photoLike approximates a photograph for the encoders: smooth gradients
// with sensor-like noise. With cutout, the outer two thirds are transparent.
func photoLike(w, h int, cutout bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewPCG(1, 2))
	for y := range h {
		for x := range w {
			n := rng.IntN(13) - 6
			a := uint8(255)
			if cutout && (x < w/3 || x > 2*w/3) {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{clamp(60 + 150*x/w + n), clamp(80 + 120*y/h + n), clamp(170 - 90*(x+y)/(w+h) + n), a})
		}
	}
	return img
}

func clamp(v int) uint8 { return uint8(min(255, max(0, v))) }

func writeImage(t *testing.T, path string, encode func(*os.File) error) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := encode(f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func mib(n int64) string {
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MiB"
}
