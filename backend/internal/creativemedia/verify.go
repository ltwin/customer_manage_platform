package creativemedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"
	_ "golang.org/x/image/webp"
)

// Format is one enabled MIME/container combination. Video and audio entries
// require ffprobe; absence removes them from capabilities instead of faking.
type Format struct {
	Kind       string   `json:"kind"`
	Mime       string   `json:"mime"`
	Extensions []string `json:"extensions"`
	Codecs     []string `json:"codecs,omitempty"`
}

func imageFormats() []Format {
	return []Format{
		{Kind: "image", Mime: "image/jpeg", Extensions: []string{".jpg", ".jpeg"}},
		{Kind: "image", Mime: "image/png", Extensions: []string{".png"}},
		{Kind: "image", Mime: "image/webp", Extensions: []string{".webp"}},
	}
}
func avFormats() []Format {
	return []Format{
		{Kind: "video", Mime: "video/mp4", Extensions: []string{".mp4", ".m4v"}, Codecs: []string{"h264", "hevc", "av1", "aac", "mp3"}},
		{Kind: "video", Mime: "video/webm", Extensions: []string{".webm"}, Codecs: []string{"vp8", "vp9", "av1", "opus", "vorbis"}},
		{Kind: "audio", Mime: "audio/mpeg", Extensions: []string{".mp3"}, Codecs: []string{"mp3"}},
		{Kind: "audio", Mime: "audio/wav", Extensions: []string{".wav"}, Codecs: []string{"pcm_s16le", "pcm_s24le", "pcm_s32le", "pcm_f32le", "pcm_u8"}},
	}
}

// Verified is the trusted result of a full-content check.
type Verified struct {
	Mime       string
	Kind       string
	SHA256     string
	Size       int64
	Width      *int
	Height     *int
	DurationMs *int64
	Codecs     json.RawMessage
	// Display is an optional derived rendition (images only).
	Display *Rendition
}
type Rendition struct {
	Path   string
	Mime   string
	SHA256 string
	Size   int64
	Width  int
	Height int
}

// Verifier decides formats by content and probes real media, never by the
// declared MIME, the file name or a multipart ETag.
type Verifier struct {
	cfg      Config
	ffprobe  string
	avEnable bool
}

func NewVerifier(cfg Config) *Verifier {
	v := &Verifier{cfg: cfg}
	if cfg.FFProbe != "" {
		if resolved, err := exec.LookPath(cfg.FFProbe); err == nil {
			v.ffprobe = resolved
			v.avEnable = true
		}
	}
	return v
}

// Formats lists what this process can actually verify right now.
func (v *Verifier) Formats() []Format {
	result := imageFormats()
	if v.avEnable {
		result = append(result, avFormats()...)
	}
	return result
}
func (v *Verifier) format(mime string) (Format, bool) {
	for _, f := range v.Formats() {
		if f.Mime == mime {
			return f, true
		}
	}
	return Format{}, false
}
func (v *Verifier) MaxBytes(kind string) int64 {
	if kind == "image" {
		return v.cfg.ImageMaxBytes
	}
	return v.cfg.AVMaxBytes
}

// Verify streams the fixed staging object into a private temporary file,
// computes the real hash/size, sniffs the container and probes the media.
// The caller removes the returned temporary files after publication.
func (v *Verifier) Verify(ctx context.Context, body io.Reader, declaredKind, declaredMime string, limit int64) (Verified, string, error) {
	tmp, err := os.CreateTemp("", "creative-media-verify-*")
	if err != nil {
		return Verified{}, "", err
	}
	path := tmp.Name()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(body, limit+1))
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return Verified{}, "", err
	}
	if size == 0 || size > limit {
		_ = os.Remove(path)
		return Verified{}, "", ErrSizeLimit
	}
	result := Verified{SHA256: "sha256-" + hex.EncodeToString(hasher.Sum(nil)), Size: size}
	head := make([]byte, 3072)
	f, err := os.Open(path)
	if err != nil {
		_ = os.Remove(path)
		return Verified{}, "", err
	}
	n, _ := io.ReadFull(f, head)
	_ = f.Close()
	detected := mimetype.Detect(head[:n])
	mime := detected.String()
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "audio/x-wav", "audio/vnd.wave", "audio/wave":
		mime = "audio/wav"
	case "video/x-m4v":
		mime = "video/mp4"
	}
	f2, ok := v.format(mime)
	if !ok || f2.Kind != declaredKind || declaredMime != "" && declaredMime != mime {
		_ = os.Remove(path)
		return Verified{}, "", fmt.Errorf("%w: detected %s", ErrUnsupported, mime)
	}
	result.Mime = mime
	result.Kind = f2.Kind
	if f2.Kind == "image" {
		if err := v.verifyImage(path, &result); err != nil {
			_ = os.Remove(path)
			return Verified{}, "", err
		}
		return result, path, nil
	}
	if err := v.probe(ctx, path, f2, &result); err != nil {
		_ = os.Remove(path)
		return Verified{}, "", err
	}
	return result, path, nil
}

func (v *Verifier) verifyImage(path string, out *Verified) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if out.Mime == "image/webp" && isAnimatedWebP(raw) {
		return fmt.Errorf("%w: animated webp", ErrUnsupported)
	}
	config, actual, err := image.DecodeConfig(bytes.NewReader(raw))
	want := map[string]string{"image/jpeg": "jpeg", "image/png": "png", "image/webp": "webp"}[out.Mime]
	if err != nil || actual != want {
		return fmt.Errorf("%w: image decode", ErrUnsupported)
	}
	if !v.validDimensions(config.Width, config.Height) {
		return fmt.Errorf("%w: image dimensions", ErrSizeLimit)
	}
	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return fmt.Errorf("%w: image decode", ErrUnsupported)
	}
	w, h := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if !v.validDimensions(w, h) {
		return fmt.Errorf("%w: image dimensions", ErrSizeLimit)
	}
	out.Width, out.Height = &w, &h
	out.Codecs = json.RawMessage(`{"format":"` + actual + `"}`)
	if w <= v.cfg.DisplayMaxEdge && h <= v.cfg.DisplayMaxEdge {
		return nil
	}
	if w >= h {
		decoded = imaging.Resize(decoded, v.cfg.DisplayMaxEdge, 0, imaging.Lanczos)
	} else {
		decoded = imaging.Resize(decoded, 0, v.cfg.DisplayMaxEdge, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, decoded, &jpeg.Options{Quality: 86}); err != nil {
		return err
	}
	display, err := os.CreateTemp("", "creative-media-display-*")
	if err != nil {
		return err
	}
	_, err = display.Write(buf.Bytes())
	if closeErr := display.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(display.Name())
		return err
	}
	sum := sha256.Sum256(buf.Bytes())
	out.Display = &Rendition{Path: display.Name(), Mime: "image/jpeg", SHA256: "sha256-" + hex.EncodeToString(sum[:]), Size: int64(buf.Len()), Width: decoded.Bounds().Dx(), Height: decoded.Bounds().Dy()}
	return nil
}
func (v *Verifier) validDimensions(w, h int) bool {
	return w > 0 && h > 0 && w <= v.cfg.MaxImageEdge && h <= v.cfg.MaxImageEdge && int64(w)*int64(h) <= v.cfg.MaxImagePixels
}
func isAnimatedWebP(raw []byte) bool {
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
		return false
	}
	for offset := 12; offset+8 <= len(raw); {
		chunk := string(raw[offset : offset+4])
		size := int(raw[offset+4]) | int(raw[offset+5])<<8 | int(raw[offset+6])<<16 | int(raw[offset+7])<<24
		if size < 0 || offset+8+size > len(raw) {
			return false
		}
		if chunk == "ANIM" || chunk == "ANMF" || (chunk == "VP8X" && size >= 1 && raw[offset+8]&0x02 != 0) {
			return true
		}
		offset += 8 + size + size%2
	}
	return false
}

type probeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

// probe runs ffprobe with a bounded deadline and only reads stream metadata;
// no transcoding or full decoding happens here.
func (v *Verifier) probe(ctx context.Context, path string, f Format, out *Verified) error {
	if !v.avEnable {
		return ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, v.cfg.MaxProbeDuration)
	defer cancel()
	cmd := exec.CommandContext(ctx, v.ffprobe, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	raw, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w: probe failed", ErrUnsupported)
		}
		return err
	}
	var p probeOutput
	if err := json.Unmarshal(raw, &p); err != nil || len(p.Streams) == 0 {
		return fmt.Errorf("%w: probe output", ErrUnsupported)
	}
	allowed := map[string]bool{}
	for _, c := range f.Codecs {
		allowed[c] = true
	}
	var width, height int
	hasVideo, hasAudio := false, false
	codecs := map[string]string{}
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if s.CodecName == "mjpeg" || s.CodecName == "png" {
				continue // Embedded cover art is not a video track.
			}
			hasVideo = true
			if width == 0 {
				width, height = s.Width, s.Height
			}
			codecs["video"] = s.CodecName
		case "audio":
			hasAudio = true
			codecs["audio"] = s.CodecName
		default:
			continue
		}
		if !allowed[s.CodecName] {
			return fmt.Errorf("%w: codec %s", ErrUnsupported, s.CodecName)
		}
	}
	if f.Kind == "video" && (!hasVideo || width <= 0 || height <= 0) || f.Kind == "audio" && (!hasAudio || hasVideo) {
		return fmt.Errorf("%w: stream layout", ErrUnsupported)
	}
	if f.Kind == "video" {
		if width > v.cfg.MaxImageEdge || height > v.cfg.MaxImageEdge {
			return fmt.Errorf("%w: video dimensions", ErrSizeLimit)
		}
		out.Width, out.Height = &width, &height
	}
	if p.Format.Duration != "" {
		seconds, err := strconv.ParseFloat(p.Format.Duration, 64)
		if err == nil && seconds >= 0 && !math.IsInf(seconds, 0) {
			ms := int64(math.Round(seconds * 1000))
			out.DurationMs = &ms
		}
	}
	if out.DurationMs == nil {
		zero := int64(0)
		out.DurationMs = &zero
	}
	codecs["container"] = p.Format.FormatName
	encoded, _ := json.Marshal(codecs)
	out.Codecs = encoded
	return nil
}
