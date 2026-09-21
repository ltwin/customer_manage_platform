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

// stageBody 把精确字节流进一个私有临时文件，同时计算真实哈希与大小。
// 调用方负责删除该文件。
func stageBody(body io.Reader, limit int64) (string, int64, string, error) {
	tmp, err := os.CreateTemp("", "creative-media-verify-*")
	if err != nil {
		return "", 0, "", err
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
		return "", 0, "", err
	}
	if size == 0 || size > limit {
		_ = os.Remove(path)
		return "", 0, "", ErrSizeLimit
	}
	return path, size, "sha256-" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// detectStoredMime 从落盘字节嗅探容器类型，绝不采信声明的 MIME 或文件
// 名，并归一化别名字形。
func detectStoredMime(path string) (string, error) {
	head := make([]byte, 3072)
	f, err := os.Open(path)
	if err != nil {
		return "", err
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
	return mime, nil
}

// Verify streams the fixed staging object into a private temporary file,
// computes the real hash/size, sniffs the container and probes the media.
// The caller removes the returned temporary files after publication.
func (v *Verifier) Verify(ctx context.Context, body io.Reader, declaredKind, declaredMime string, limit int64) (Verified, string, error) {
	path, size, sha, err := stageBody(body, limit)
	if err != nil {
		return Verified{}, "", err
	}
	result := Verified{SHA256: sha, Size: size}
	mime, err := detectStoredMime(path)
	if err != nil {
		_ = os.Remove(path)
		return Verified{}, "", err
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
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		RFrameRate   string `json:"r_frame_rate"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

// parseProbeDuration 把 ffprobe 的秒字面量换算成取整的毫秒。缺失、无法
// 解析或不合理的输入一律报告未知，绝不报零；上限保证 int64 换算良定义。
func parseProbeDuration(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(s, 64)
	if err != nil || seconds < 0 || seconds > maxProbeSeconds || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return 0, false
	}
	return int64(math.Round(seconds * 1000)), true
}

// maxProbeSeconds 封顶可信的媒体时长（24h）；超出即视为不可用的探测
// 值，而不是事实。
const maxProbeSeconds = 24 * 60 * 60

// parseProbeRational 把 ffprobe 的 "num/den"（或裸 "num"）字面量换算成
// 精确的整数有理数。"0/0"、"N/A" 与零分量报告未知；缺分母的裸数按
// den=1 处理。
func parseProbeRational(s string) (int64, int64, bool) {
	numText, denText, hasDen := strings.Cut(s, "/")
	num, err := strconv.ParseInt(numText, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	den := int64(1)
	if hasDen {
		if den, err = strconv.ParseInt(denText, 10, 64); err != nil {
			return 0, 0, false
		}
	}
	if num <= 0 || den <= 0 {
		return 0, 0, false
	}
	return num, den, true
}

// ProbedFacts 是一次受控探测的版本化结果。nil 度量字段表示「本提取器
// 下未确立」；零是已知值，绝不顶替未知。DimensionBasis 记录尺寸的语义：
// "oriented" 是实际字节经方向修正后的显示尺寸，"container" 是流容器
// 尺寸。
type ProbedFacts struct {
	Mime           string
	Kind           string
	SHA256         string
	Size           int64
	Width          *int
	Height         *int
	DimensionBasis string
	DurationMs     *int64
	FrameRateNum   int64
	FrameRateDen   int64
}

// Probe 把精确对象字节落盘一次，校验内容哈希与容器，并提取版本化事实。
// 与 Verify 不同，它不再对照声明的类型/MIME 校验字节（blob 行在上传时
// 已做过），也容忍可选元数据缺失——未知保持未知；只有身份（哈希、容器、
// 大小）是硬失败。帧率依赖 ffprobe，缺它时报告未知。
func (v *Verifier) Probe(ctx context.Context, body io.Reader, limit int64) (ProbedFacts, error) {
	path, size, sha, err := stageBody(body, limit)
	if err != nil {
		return ProbedFacts{}, err
	}
	defer func() { _ = os.Remove(path) }()
	result := ProbedFacts{SHA256: sha, Size: size}
	mime, err := detectStoredMime(path)
	if err != nil {
		return ProbedFacts{}, err
	}
	f, ok := v.format(mime)
	if !ok {
		return ProbedFacts{}, fmt.Errorf("%w: detected %s", ErrUnsupported, mime)
	}
	result.Mime, result.Kind = mime, f.Kind
	if f.Kind == "image" {
		if err := v.probeImage(path, &result); err != nil {
			return ProbedFacts{}, err
		}
		return result, nil
	}
	if f.Kind == "audio" {
		result.DimensionBasis = "none"
	}
	if !v.avEnable {
		return ProbedFacts{}, fmt.Errorf("%w: probe tooling unavailable", ErrUnsupported)
	}
	return v.probeAV(ctx, path, f.Kind, &result)
}

// probeImage 按上传校验相同的方向修正解码，事实携带的就是将要发送的
// 精确字节的 oriented 显示语义。
func (v *Verifier) probeImage(path string, out *ProbedFacts) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if out.Mime == "image/webp" && isAnimatedWebP(raw) {
		return fmt.Errorf("%w: animated webp", ErrUnsupported)
	}
	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return fmt.Errorf("%w: image decode", ErrUnsupported)
	}
	w, h := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%w: image dimensions", ErrUnsupported)
	}
	out.Width, out.Height = &w, &h
	out.DimensionBasis = "oriented"
	return nil
}

// probeAV 运行 ffprobe 取时长、容器尺寸与帧率。超时或拉起失败属瞬时
// 故障（调用方按预算重试）；只有 ffprobe 本身拒绝该媒体才是
// ErrUnsupported。未能确立尺寸的视频组不成合法事实行，按 unsupported
// 上报；音频按模态不携带尺寸。
func (v *Verifier) probeAV(ctx context.Context, path, kind string, out *ProbedFacts) (ProbedFacts, error) {
	ctx, cancel := context.WithTimeout(ctx, v.cfg.MaxProbeDuration)
	defer cancel()
	cmd := exec.CommandContext(ctx, v.ffprobe, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	raw, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		switch {
		case ctx.Err() != nil:
			return ProbedFacts{}, fmt.Errorf("creative media probe timed out: %w", err)
		case errors.As(err, &exit):
			// ffprobe 跑过并拒绝了这些字节：永久性的格式判定。
			return ProbedFacts{}, fmt.Errorf("%w: probe failed", ErrUnsupported)
		default:
			return ProbedFacts{}, fmt.Errorf("creative media probe failed to run: %w", err)
		}
	}
	var p probeOutput
	if err := json.Unmarshal(raw, &p); err != nil {
		// ffprobe 正常退出但输出不可用：工具异常，不是对媒体的判定。
		return ProbedFacts{}, fmt.Errorf("creative media probe output unreadable: %w", err)
	}
	if len(p.Streams) == 0 {
		return ProbedFacts{}, fmt.Errorf("%w: probe output", ErrUnsupported)
	}
	hasVideo := false
	for _, s := range p.Streams {
		if s.CodecType != "video" || s.CodecName == "mjpeg" || s.CodecName == "png" {
			continue // 内嵌封面不是视频轨。
		}
		hasVideo = true
		if s.Width > 0 && s.Height > 0 {
			w, h := s.Width, s.Height
			out.Width, out.Height = &w, &h
			out.DimensionBasis = "container"
		}
		if num, den, ok := parseProbeRational(s.AvgFrameRate); ok {
			out.FrameRateNum, out.FrameRateDen = num, den
		} else if num, den, ok := parseProbeRational(s.RFrameRate); ok {
			out.FrameRateNum, out.FrameRateDen = num, den
		}
		break
	}
	if kind == "video" && (!hasVideo || out.Width == nil) {
		return ProbedFacts{}, fmt.Errorf("%w: stream layout", ErrUnsupported)
	}
	if ms, ok := parseProbeDuration(p.Format.Duration); ok {
		out.DurationMs = &ms
	}
	return *out, nil
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
				continue // 内嵌封面不是视频轨。
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
	if ms, ok := parseProbeDuration(p.Format.Duration); ok {
		out.DurationMs = &ms
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
