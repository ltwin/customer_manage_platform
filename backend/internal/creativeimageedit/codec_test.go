package creativeimageedit

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/disintegration/imaging"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	_ "golang.org/x/image/webp"
)

// Rendering tests run the real vips binaries. Without them they skip; the
// admission tests still run, and `make check-image-codec` runs the full suite.
func newCodec(t *testing.T) *Codec {
	t.Helper()
	if _, err := exec.LookPath("vips"); err != nil {
		t.Skip("vips not installed; run make check-image-codec")
	}
	cfg := CodecConfig{Vips: "vips", VipsHeader: "vipsheader", TempRoot: t.TempDir(), MaxEdge: 12000, MaxPixels: 60_000_000}
	if runtime.GOOS == "linux" {
		cfg.Choom = "choom"
	}
	c, err := NewCodec(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// quadrants paints four distinct corners so any rotation or mirror shows.
var quadrants = [4]color.NRGBA{{220, 30, 30, 255}, {30, 200, 40, 255}, {30, 40, 210, 255}, {240, 240, 240, 255}}

func pattern(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			q := 0
			if x >= w/2 {
				q++
			}
			if y >= h/2 {
				q += 2
			}
			c := quadrants[q]
			// Low-amplitude texture keeps the encoders honest about size.
			n := uint8((x*7 + y*13) % 9)
			img.SetNRGBA(x, y, color.NRGBA{c.R - n, c.G + n, c.B + n%3, c.A})
		}
	}
	return img
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func encodeJPEG(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withOrientation inserts an EXIF APP1 segment carrying only the
// orientation tag right after SOI.
func withOrientation(data []byte, orientation int) []byte {
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, byte(orientation), 0, 0, 0, 0, 0, 0, 0}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	return append(append(append([]byte{}, data[:2]...), segment...), data[2:]...)
}

// withChunk inserts a PNG chunk right after IHDR.
func withChunk(data []byte, kind string, body []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(chunk, kind...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(append([]byte(kind), body...)))
	const afterIHDR = 8 + 25
	return append(append(append([]byte{}, data[:afterIHDR]...), chunk...), data[afterIHDR:]...)
}

// vipsTool makes samples Go cannot encode (WebP, ICC, CMYK, opaque RGBA).
func vipsTool(t *testing.T, c *Codec, args ...string) {
	t.Helper()
	if out, err := exec.Command(c.cfg.Vips, args...).CombinedOutput(); err != nil {
		t.Fatalf("vips %v: %v: %s", args, err, out)
	}
}

func decode(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func near(a, b color.Color, tolerance int) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	for _, d := range []int{int(ar>>8) - int(br>>8), int(ag>>8) - int(bg>>8), int(ab>>8) - int(bb>>8), int(aa>>8) - int(ba>>8)} {
		if d < -tolerance || d > tolerance {
			return false
		}
	}
	return true
}

func render(t *testing.T, c *Codec, req Request) Result {
	t.Helper()
	result, err := c.Render(t.Context(), req)
	if err != nil {
		t.Fatalf("render %s %s: %v", req.Mime, req.Action, err)
	}
	t.Cleanup(func() { _ = result.Close() })
	return result
}

func headerText(t *testing.T, c *Codec, path string) string {
	t.Helper()
	out, err := exec.Command(c.cfg.VipsHeader, "-a", path).CombinedOutput()
	if err != nil {
		t.Fatalf("vipsheader: %v: %s", err, out)
	}
	return string(out)
}

func TestCompressAppliesEveryOrientationAndDropsMetadata(t *testing.T) {
	c := newCodec(t)
	plain := encodeJPEG(t, pattern(96, 48), 95)
	for orientation := 1; orientation <= 8; orientation++ {
		source := write(t, "o.jpg", withOrientation(plain, orientation))
		f, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		want, err := imaging.Decode(f, imaging.AutoOrientation(true))
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		result := render(t, c, Request{Source: source, Mime: "image/jpeg", Action: ActionCompress, Quality: 60})
		if result.Mime != "image/jpeg" || result.Width != want.Bounds().Dx() || result.Height != want.Bounds().Dy() {
			t.Fatalf("orientation %d: got %s %dx%d, want %v", orientation, result.Mime, result.Width, result.Height, want.Bounds())
		}
		if h := headerText(t, c, result.Path); strings.Contains(h, "exif-") || strings.Contains(h, "orientation: ") && !strings.Contains(h, "orientation: 1") {
			t.Fatalf("orientation %d: output kept metadata:\n%s", orientation, h)
		}
		got := decode(t, result.Path)
		w, h := result.Width, result.Height
		for _, p := range []image.Point{{w / 4, h / 4}, {3 * w / 4, h / 4}, {w / 4, 3 * h / 4}, {3 * w / 4, 3 * h / 4}} {
			if !near(got.At(p.X, p.Y), want.At(p.X, p.Y), 24) {
				t.Fatalf("orientation %d at %v: got %v, want %v", orientation, p, color.NRGBAModel.Convert(got.At(p.X, p.Y)), color.NRGBAModel.Convert(want.At(p.X, p.Y)))
			}
		}
	}
}

func TestCompressFollowsTheFormatPolicy(t *testing.T) {
	c := newCodec(t)
	img := pattern(160, 120)
	transparent := pattern(160, 120)
	for y := range 40 {
		for x := range 160 {
			transparent.SetNRGBA(x, y, color.NRGBA{})
		}
	}
	gray := image.NewGray(img.Bounds())
	deep := image.NewNRGBA64(img.Bounds())
	for y := range 120 {
		for x := range 160 {
			gray.Set(x, y, img.At(x, y))
			deep.Set(x, y, img.At(x, y))
		}
	}
	opaqueRGB := write(t, "rgb.png", encodePNG(t, img))
	opaqueRGBA := filepath.Join(t.TempDir(), "rgba.png")
	vipsTool(t, c, "bandjoin_const", opaqueRGB, opaqueRGBA, "255")
	webp := filepath.Join(t.TempDir(), "plain.webp")
	vipsTool(t, c, "webpsave", opaqueRGB, webp, "--lossless")
	cases := []struct {
		name, source, mime, want string
	}{
		{"jpeg", write(t, "a.jpg", encodeJPEG(t, img, 95)), "image/jpeg", "image/jpeg"},
		{"opaque png", opaqueRGB, "image/png", "image/jpeg"},
		{"opaque png with alpha band", opaqueRGBA, "image/png", "image/jpeg"},
		{"transparent png", write(t, "t.png", encodePNG(t, transparent)), "image/png", "image/webp"},
		{"gray png", write(t, "g.png", encodePNG(t, gray)), "image/png", "image/jpeg"},
		{"16-bit png", write(t, "d.png", encodePNG(t, deep)), "image/png", "image/jpeg"},
		{"webp", webp, "image/webp", "image/webp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := render(t, c, Request{Source: tc.source, Mime: tc.mime, Action: ActionCompress, Quality: 70})
			if result.Mime != tc.want || result.Width != 160 || result.Height != 120 {
				t.Fatalf("got %s %dx%d, want %s 160x120", result.Mime, result.Width, result.Height, tc.want)
			}
			if !strings.HasSuffix(result.Path, map[string]string{"image/jpeg": ".jpg", "image/webp": ".webp"}[tc.want]) {
				t.Fatalf("path %s does not match %s", result.Path, tc.want)
			}
			got := decode(t, result.Path)
			if !near(got.At(120, 100), img.At(120, 100), 24) {
				t.Fatalf("pixel %v, want near %v", color.NRGBAModel.Convert(got.At(120, 100)), img.At(120, 100))
			}
			if tc.name == "transparent png" {
				if _, _, _, a := got.At(10, 10).RGBA(); a != 0 {
					t.Fatalf("transparent pixel alpha %d, want 0 (no background colour)", a>>8)
				}
			}
		})
	}
}

func TestCropIsLosslessWhereThePolicySaysSo(t *testing.T) {
	c := newCodec(t)
	img := pattern(120, 80)
	pngSource := write(t, "a.png", encodePNG(t, img))
	webp := filepath.Join(t.TempDir(), "a.webp")
	vipsTool(t, c, "webpsave", pngSource, webp, "--lossless")
	box := Crop{Left: 50, Top: 30, Width: 40, Height: 30}
	for _, tc := range []struct{ source, mime string }{{pngSource, "image/png"}, {webp, "image/webp"}} {
		result := render(t, c, Request{Source: tc.source, Mime: tc.mime, Action: ActionCrop, Crop: box})
		if result.Mime != tc.mime || result.Width != 40 || result.Height != 30 {
			t.Fatalf("%s: got %s %dx%d", tc.mime, result.Mime, result.Width, result.Height)
		}
		got := decode(t, result.Path)
		for y := range 30 {
			for x := range 40 {
				if !near(got.At(x, y), img.At(box.Left+x, box.Top+y), 0) {
					t.Fatalf("%s at %d,%d: got %v, want %v", tc.mime, x, y, got.At(x, y), img.At(box.Left+x, box.Top+y))
				}
			}
		}
	}
}

func TestCropBoxIsInDisplayOrientation(t *testing.T) {
	c := newCodec(t)
	// Orientation 6 displays the stored 96x48 image as 48x96.
	source := write(t, "o.jpg", withOrientation(encodeJPEG(t, pattern(96, 48), 95), 6))
	result := render(t, c, Request{Source: source, Mime: "image/jpeg", Action: ActionCrop, Crop: Crop{Left: 0, Top: 50, Width: 48, Height: 46}})
	if result.Mime != "image/jpeg" || result.Width != 48 || result.Height != 46 {
		t.Fatalf("got %s %dx%d", result.Mime, result.Width, result.Height)
	}
	_, err := c.Render(t.Context(), Request{Source: source, Mime: "image/jpeg", Action: ActionCrop, Crop: Crop{Left: 0, Top: 0, Width: 96, Height: 48}})
	if !errors.Is(err, ErrParameters) {
		t.Fatalf("stored-orientation box: got %v, want ErrParameters", err)
	}
}

func TestColoursAreConvertedToSRGB(t *testing.T) {
	c := newCodec(t)
	red := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			red.SetNRGBA(x, y, color.NRGBA{250, 20, 20, 255})
		}
	}
	base := write(t, "red.png", encodePNG(t, red))
	dir := t.TempDir()
	p3 := filepath.Join(dir, "p3.jpg")
	vipsTool(t, c, "icc_transform", base, p3+"[Q=98]", "p3", "--input-profile", "srgb")
	if got := decode(t, p3).At(5, 5); near(got, red.At(5, 5), 6) {
		t.Fatalf("sample is not actually in P3: %v", got)
	}
	cmyk := filepath.Join(dir, "cmyk.v")
	vipsTool(t, c, "icc_transform", base, cmyk, "cmyk", "--input-profile", "srgb")
	tagged := filepath.Join(dir, "cmyk-tagged.jpg")
	untagged := filepath.Join(dir, "cmyk-untagged.jpg")
	vipsTool(t, c, "jpegsave", cmyk, tagged, "--Q", "98")
	vipsTool(t, c, "jpegsave", cmyk, untagged, "--Q", "98", "--keep", "none")
	// P3 holds this red exactly; the CMYK round trip goes through gamut
	// mapping, so it only has to come back clearly red, not identical.
	p3WebP := filepath.Join(dir, "p3.webp")
	vipsTool(t, c, "icc_transform", base, p3WebP+"[lossless]", "p3", "--input-profile", "srgb")
	for source, tolerance := range map[string]int{p3: 8, p3WebP: 8, tagged: 24} {
		mime := "image/jpeg"
		if strings.HasSuffix(source, ".webp") {
			mime = "image/webp"
		}
		result := render(t, c, Request{Source: source, Mime: mime, Action: ActionCrop, Crop: Crop{Width: 32, Height: 32}})
		if got := decode(t, result.Path).At(5, 5); !near(got, red.At(5, 5), tolerance) {
			t.Fatalf("%s: got %v, want near %v", filepath.Base(source), color.NRGBAModel.Convert(got), red.At(5, 5))
		}
		if h := headerText(t, c, result.Path); !strings.Contains(h, "icc-profile-data") || strings.Contains(h, "interpretation: cmyk") {
			t.Fatalf("%s: output is not tagged sRGB:\n%s", filepath.Base(source), h)
		}
	}
	if _, err := c.Render(t.Context(), Request{Source: untagged, Mime: "image/jpeg", Action: ActionCrop, Crop: Crop{Width: 8, Height: 8}}); !errors.Is(err, ErrColour) {
		t.Fatalf("untagged cmyk: got %v, want ErrColour", err)
	}
	var garbage bytes.Buffer
	zw := zlib.NewWriter(&garbage)
	_, _ = zw.Write(bytes.Repeat([]byte("not a profile "), 40))
	_ = zw.Close()
	// libvips drops an unparsable profile while loading; both must still be
	// refused rather than quietly treated as sRGB.
	brokenPNG := write(t, "broken.png", withChunk(encodePNG(t, red), "iCCP", append([]byte("bad\x00\x00"), garbage.Bytes()...)))
	plainJPEG := encodeJPEG(t, red, 95)
	app2 := append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte("not a profile "), 40)...)
	segment := append([]byte{0xFF, 0xE2, byte((len(app2) + 2) >> 8), byte(len(app2) + 2)}, app2...)
	brokenJPEG := write(t, "broken.jpg", append(append([]byte{0xFF, 0xD8}, segment...), plainJPEG[2:]...))
	for source, mime := range map[string]string{brokenPNG: "image/png", brokenJPEG: "image/jpeg"} {
		if _, err := c.Render(t.Context(), Request{Source: source, Mime: mime, Action: ActionCrop, Crop: Crop{Width: 8, Height: 8}}); !errors.Is(err, ErrColour) {
			t.Fatalf("%s: got %v, want ErrColour", filepath.Base(source), err)
		}
	}
}

func TestRenderRefusesWhatThePolicyDoesNotCover(t *testing.T) {
	c := newCodec(t)
	img := pattern(64, 64)
	plainPNG := encodePNG(t, img)
	var frames gif.GIF
	for _, c := range []color.Color{color.White, color.Black} {
		frame := image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.White, color.Black})
		for i := range frame.Pix {
			frame.Pix[i] = uint8(frame.Palette.Index(c))
		}
		frames.Image = append(frames.Image, frame)
		frames.Delay = append(frames.Delay, 10)
	}
	var gifBytes bytes.Buffer
	if err := gif.EncodeAll(&gifBytes, &frames); err != nil {
		t.Fatal(err)
	}
	anim := filepath.Join(t.TempDir(), "anim.webp")
	vipsTool(t, c, "copy", write(t, "anim.gif", gifBytes.Bytes())+"[n=-1]", anim)
	small := write(t, "small.jpg", encodeJPEG(t, img, 40))
	cases := []struct {
		name string
		req  Request
		want error
	}{
		{"apng", Request{Source: write(t, "a.png", withChunk(plainPNG, "acTL", make([]byte, 8))), Mime: "image/png", Action: ActionCompress, Quality: 50}, creativemedia.ErrUnsupported},
		{"animated webp", Request{Source: anim, Mime: "image/webp", Action: ActionCompress, Quality: 50}, creativemedia.ErrUnsupported},
		{"not an image", Request{Source: write(t, "x.png", []byte("plain text")), Mime: "image/png", Action: ActionCompress, Quality: 50}, creativemedia.ErrUnsupported},
		{"unsupported mime", Request{Source: small, Mime: "image/gif", Action: ActionCompress, Quality: 50}, creativemedia.ErrUnsupported},
		{"quality", Request{Source: small, Mime: "image/jpeg", Action: ActionCompress, Quality: 0}, ErrParameters},
		{"crop outside", Request{Source: small, Mime: "image/jpeg", Action: ActionCrop, Crop: Crop{Left: 60, Width: 8, Height: 8}}, ErrParameters},
		{"not reduced", Request{Source: small, Mime: "image/jpeg", Action: ActionCompress, Quality: 100}, ErrNotReduced},
	}
	for _, tc := range cases {
		if _, err := c.Render(t.Context(), tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	tight := *c
	tight.cfg.MaxPixels = 64*64 - 1
	if _, err := tight.Render(t.Context(), Request{Source: small, Mime: "image/jpeg", Action: ActionCompress, Quality: 30}); !errors.Is(err, creativemedia.ErrSizeLimit) {
		t.Errorf("pixels: got %v, want ErrSizeLimit", err)
	}
	if left, _ := os.ReadDir(c.cfg.TempRoot); len(left) != 0 {
		t.Fatalf("failed renders left %d entries in the temp root", len(left))
	}
}

func TestCancelledRenderLeavesNothingBehind(t *testing.T) {
	c := newCodec(t)
	source := write(t, "big.png", encodePNG(t, pattern(3000, 2000)))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Render(ctx, Request{Source: source, Mime: "image/png", Action: ActionCompress, Quality: 50}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline exceeded", err)
	}
	if left, _ := os.ReadDir(c.cfg.TempRoot); len(left) != 0 {
		t.Fatalf("cancelled render left %d entries", len(left))
	}
}

func TestSelfCheck(t *testing.T) {
	c := newCodec(t)
	version, err := c.SelfCheck(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "vips-8.") {
		t.Fatalf("version %q", version)
	}
}

// This test does not need vips: exceeding either limit must be rejected
// before the codec executable is even consulted or a render directory exists.
func TestRenderRejectsOversizedSourceBeforeStartingCodec(t *testing.T) {
	img := pattern(32, 16)
	// A lossless WebP header with a 32x16 canvas, deliberately without
	// pixel data: admission only needs the dimensions, not a pixel decode.
	webpHeader := []byte("RIFF\x12\x00\x00\x00WEBPVP8L\x05\x00\x00\x00\x2f\x1f\xc0\x03\x00\x00")
	for _, source := range []struct {
		mime string
		data []byte
	}{
		{"image/jpeg", encodeJPEG(t, img, 95)},
		{"image/png", encodePNG(t, img)},
		{"image/webp", webpHeader},
	} {
		for _, limit := range []struct {
			name   string
			edge   int
			pixels int64
		}{
			{"edge", 31, 512},
			{"pixels", 32, 511},
		} {
			t.Run(source.mime+"/"+limit.name, func(t *testing.T) {
				root := t.TempDir()
				c := &Codec{cfg: CodecConfig{
					Vips:     filepath.Join(t.TempDir(), "must-not-start"),
					TempRoot: root, MaxEdge: limit.edge, MaxPixels: limit.pixels,
				}}
				_, err := c.Render(t.Context(), Request{
					Source: write(t, "source", source.data), Mime: source.mime,
					Action: ActionCompress, Quality: 60,
				})
				if !errors.Is(err, creativemedia.ErrSizeLimit) {
					t.Fatalf("want ErrSizeLimit before starting codec, got %v", err)
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("admission created render files: %v, %v", entries, err)
				}
			})
		}
	}
}

func TestSourceSizeAdmissionBoundaries(t *testing.T) {
	portrait := write(t, "portrait.png", encodePNG(t, pattern(16, 32)))
	for _, tc := range []struct {
		name, mime string
		edge       int
		pixels     int64
		want       error
	}{
		{"exact limits", "image/png", 32, 512, nil},
		{"height exceeds edge", "image/png", 31, 512, creativemedia.ErrSizeLimit},
		{"pixels exceed limit", "image/png", 32, 511, creativemedia.ErrSizeLimit},
		{"wrong format", "image/jpeg", 32, 512, creativemedia.ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Codec{cfg: CodecConfig{MaxEdge: tc.edge, MaxPixels: tc.pixels}}
			if err := c.admitSourceSize(portrait, tc.mime); !errors.Is(err, tc.want) {
				t.Fatalf("source admission: got %v, want %v", err, tc.want)
			}
		})
	}
}
