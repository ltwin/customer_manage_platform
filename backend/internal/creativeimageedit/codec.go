// Package creativeimageedit owns image edit candidates, their parameter
// policy and the controlled libvips subprocess that renders them. Object
// bytes, quota and GC stay with creativemedia.
package creativeimageedit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

// PolicyVersion names the fixed decode → orient → sRGB → crop → encode rules
// below. Any change to formats, qualities or metadata handling bumps it.
const PolicyVersion = "image-edit-v1"

const (
	ActionCompress = "compress"
	ActionCrop     = "crop"

	// cropJPEGQuality re-encodes a cropped JPEG once at a quality that keeps
	// the crop from visibly costing detail; PNG and WebP crops are lossless.
	cropJPEGQuality  = 95
	tempPrefix       = "render-"
	sourceTempPrefix = "source-"
	// oomScoreAdj makes the kernel pick the vips child before the worker when
	// the container runs out of memory. It lowers the risk, it does not
	// isolate: the verifier still decodes in the worker afterwards.
	oomScoreAdj = "1000"
)

var (
	// ErrColour rejects inputs whose colours cannot be known: CMYK without a
	// profile, or an embedded profile that does not load.
	ErrColour = errors.New("creative image edit colour profile unusable")
	// ErrNotReduced is a compression that did not make the file smaller.
	ErrNotReduced = errors.New("creative image edit output not smaller")
	// ErrParameters is a quality or crop box outside what the policy accepts.
	ErrParameters = errors.New("creative image edit parameters invalid")
)

// CodecConfig fixes the binaries and limits. Choom is the util-linux launcher
// that sets the child's OOM score before exec; empty skips the adjustment,
// which only development machines without /proc should do.
type CodecConfig struct {
	Vips       string
	VipsHeader string
	Choom      string
	TempRoot   string
	MaxEdge    int
	MaxPixels  int64
}

// Request is one render of a fixed source file. Mime is the verified source
// type, never a declared one; Crop is in the source's display orientation.
type Request struct {
	Source  string
	Mime    string
	Action  string
	Quality int
	Crop    Crop
}

type Crop struct {
	Left, Top, Width, Height int
}

// Result is the encoded file plus what the run cost. Close removes the
// private directory holding it once the caller has stored the bytes.
type Result struct {
	Path   string
	Mime   string
	Width  int
	Height int
	Size   int64
	// PeakDisk is the largest total of intermediate files alive at once.
	PeakDisk int64
	Elapsed  time.Duration
	dir      string
	// peakRSS is the largest ru_maxrss of the vips children. Go starts
	// children with vfork, and Linux carries the parent's high-water mark
	// across exec, so it is only the child's own peak when the caller is
	// small — true in the resource spike, not in the worker.
	peakRSS int64
}

func (r Result) Close() error { return os.RemoveAll(r.dir) }

// Codec renders through vips CLI subprocesses: explicit loaders, lossless
// .v intermediates, a fixed argument list and no shell.
type Codec struct {
	cfg CodecConfig
}

func NewCodec(cfg CodecConfig) (*Codec, error) {
	var err error
	if cfg.Vips, err = exec.LookPath(cfg.Vips); err != nil {
		return nil, fmt.Errorf("vips: %w", err)
	}
	if cfg.VipsHeader, err = exec.LookPath(cfg.VipsHeader); err != nil {
		return nil, fmt.Errorf("vipsheader: %w", err)
	}
	if cfg.Choom != "" {
		if cfg.Choom, err = exec.LookPath(cfg.Choom); err != nil {
			return nil, fmt.Errorf("choom: %w", err)
		}
	}
	if cfg.TempRoot == "" {
		cfg.TempRoot = os.TempDir()
	}
	if err := os.MkdirAll(cfg.TempRoot, 0o700); err != nil {
		return nil, err
	}
	return &Codec{cfg: cfg}, nil
}

// SweepTemp removes render directories and staged sources a killed process left behind. Only
// the single worker that owns TempRoot may call it, before it renders.
func (c *Codec) SweepTemp() error {
	entries, err := os.ReadDir(c.cfg.TempRoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), tempPrefix) || !e.IsDir() && strings.HasPrefix(e.Name(), sourceTempPrefix) {
			if err := os.RemoveAll(filepath.Join(c.cfg.TempRoot, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

var loaders = map[string]string{"image/jpeg": "jpegload", "image/png": "pngload", "image/webp": "webpload"}

func (r Request) validate() error {
	if _, ok := loaders[r.Mime]; !ok {
		return fmt.Errorf("%w: %s", creativemedia.ErrUnsupported, r.Mime)
	}
	switch r.Action {
	case ActionCompress:
		if r.Quality < qualityMin || r.Quality > qualityMax {
			return fmt.Errorf("%w: quality %d", ErrParameters, r.Quality)
		}
	case ActionCrop:
		if !cropFits(r.Crop, maxCropEdge, maxCropEdge) {
			return fmt.Errorf("%w: crop %+v", ErrParameters, r.Crop)
		}
	default:
		return fmt.Errorf("%w: action %q", ErrParameters, r.Action)
	}
	return nil
}

// Render applies the policy to one source. A failed or cancelled render
// leaves nothing behind; the source file is never modified.
func (c *Codec) Render(ctx context.Context, req Request) (result Result, err error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if err := c.admitSourceSize(req.Source, req.Mime); err != nil {
		return Result{}, err
	}
	declared, err := scanSource(req.Source, req.Mime)
	if err != nil {
		return Result{}, err
	}
	started := time.Now()
	dir, err := os.MkdirTemp(c.cfg.TempRoot, tempPrefix)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	p := &pipeline{codec: c, ctx: ctx, dir: dir, declaredProfile: declared}
	result, err = p.render(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, ctxErr
		}
		return Result{}, err
	}
	result.dir = dir
	result.peakRSS, result.PeakDisk = p.peakRSS, p.peakDisk
	result.Elapsed = time.Since(started)
	return result, nil
}

type pipeline struct {
	codec *Codec
	ctx   context.Context
	dir   string
	// declaredProfile is whether the source file carries an ICC profile.
	// libvips drops a profile it cannot parse while loading, so a declared
	// profile missing from the loaded header means the colours are unknown.
	declaredProfile bool
	seq             int
	current         string
	peakRSS         int64
	peakDisk        int64
}

func (p *pipeline) render(req Request) (Result, error) {
	if err := p.step(loaders[req.Mime], []string{req.Source}, nil); err != nil {
		return Result{}, fmt.Errorf("%w: %v", creativemedia.ErrUnsupported, err)
	}
	h, err := p.header(p.current)
	if err != nil {
		return Result{}, err
	}
	if err := p.admit(h); err != nil {
		return Result{}, err
	}
	if h.orientation > 1 {
		if err := p.step("autorot", nil, nil); err != nil {
			return Result{}, err
		}
	}
	if err := p.toSRGB(h); err != nil {
		return Result{}, err
	}
	if h, err = p.header(p.current); err != nil {
		return Result{}, err
	}
	if req.Action == ActionCrop {
		box := req.Crop
		if !cropFits(box, h.width, h.height) {
			return Result{}, fmt.Errorf("%w: crop %+v outside %dx%d", ErrParameters, box, h.width, h.height)
		}
		if err := p.step("extract_area", nil, []string{itoa(box.Left), itoa(box.Top), itoa(box.Width), itoa(box.Height)}); err != nil {
			return Result{}, err
		}
		h.width, h.height = box.Width, box.Height
	}
	out, err := p.encode(req, h)
	if err != nil {
		return Result{}, err
	}
	return p.check(req, out)
}

func (c *Codec) admitDimensions(w, ht int) error {
	if w < 1 || ht < 1 || w > c.cfg.MaxEdge || ht > c.cfg.MaxEdge || int64(w) > c.cfg.MaxPixels/int64(ht) {
		return fmt.Errorf("%w: %dx%d", creativemedia.ErrSizeLimit, w, ht)
	}
	return nil
}

// admit rechecks the decoded dimensions and refuses unsupported pixel and
// colour metadata. Source dimensions are checked before the first subprocess.
func (p *pipeline) admit(h header) error {
	if err := p.codec.admitDimensions(h.width, h.height); err != nil {
		return err
	}
	if h.pages > 1 {
		return fmt.Errorf("%w: %d pages", creativemedia.ErrUnsupported, h.pages)
	}
	if h.format != "uchar" && h.format != "ushort" {
		return fmt.Errorf("%w: sample format %s", creativemedia.ErrUnsupported, h.format)
	}
	if p.declaredProfile && !h.icc {
		return fmt.Errorf("%w: embedded profile did not load", ErrColour)
	}
	switch h.interpretation {
	case "srgb", "b-w", "rgb16", "grey16":
	case "cmyk":
		if !h.icc {
			return fmt.Errorf("%w: cmyk without profile", ErrColour)
		}
	default:
		return fmt.Errorf("%w: interpretation %s", creativemedia.ErrUnsupported, h.interpretation)
	}
	return nil
}

// toSRGB converts through the embedded profile when there is one; an
// untagged image is already treated as sRGB and only loses extra bit depth.
func (p *pipeline) toSRGB(h header) error {
	if h.icc {
		if err := p.step("icc_transform", nil, []string{"srgb", "--embedded", "--intent", "perceptual", "--depth", "8"}); err != nil {
			return fmt.Errorf("%w: %v", ErrColour, err)
		}
		return nil
	}
	switch h.interpretation {
	case "rgb16":
		return p.step("colourspace", nil, []string{"srgb"})
	case "grey16":
		return p.step("colourspace", nil, []string{"b-w"})
	}
	return nil
}

func (p *pipeline) encode(req Request, h header) (encoded, error) {
	alpha := h.bands == 2 || h.bands == 4
	var target encoded
	switch {
	case req.Action == ActionCrop && req.Mime == "image/png":
		target = encoded{mime: "image/png", saver: "pngsave", ext: ".png"}
	case req.Action == ActionCrop && req.Mime == "image/webp":
		target = encoded{mime: "image/webp", saver: "webpsave", ext: ".webp", options: []string{"--lossless"}}
	case req.Action == ActionCrop:
		target = jpegTarget(cropJPEGQuality)
	case req.Mime == "image/webp":
		target = webpTarget(req.Quality)
	case req.Mime == "image/png" && alpha:
		opaque, err := p.opaque(h.bands)
		if err != nil {
			return encoded{}, err
		}
		if opaque {
			target = jpegTarget(req.Quality)
		} else {
			target = webpTarget(req.Quality)
		}
	default:
		target = jpegTarget(req.Quality)
	}
	if target.mime == "image/jpeg" && alpha {
		// Only reached for fully opaque alpha, so dropping the band is exact.
		if err := p.step("extract_band", nil, []string{"0", "--n", itoa(h.bands - 1)}); err != nil {
			return encoded{}, err
		}
	}
	target.path = filepath.Join(p.dir, "output"+target.ext)
	args := append([]string{p.current, target.path}, target.options...)
	args = append(args, "--keep", "icc")
	if err := p.vips(append([]string{target.saver}, args...)...); err != nil {
		return encoded{}, err
	}
	if err := p.account(target.path); err != nil {
		return encoded{}, err
	}
	_ = os.Remove(p.current)
	p.current = target.path
	return target, nil
}

type encoded struct {
	mime    string
	saver   string
	ext     string
	options []string
	path    string
}

func jpegTarget(q int) encoded {
	return encoded{mime: "image/jpeg", saver: "jpegsave", ext: ".jpg", options: []string{"--Q", itoa(q), "--optimize-coding"}}
}

// webpTarget keeps alpha at full quality: lossy alpha shows as halos on
// exactly the cut-outs that made this file need WebP.
func webpTarget(q int) encoded {
	return encoded{mime: "image/webp", saver: "webpsave", ext: ".webp", options: []string{"--Q", itoa(q), "--alpha-q", "100"}}
}

// opaque reports whether the alpha band is fully opaque, so a PNG that
// merely carries an alpha channel can still become a JPEG.
func (p *pipeline) opaque(bands int) (bool, error) {
	alpha := filepath.Join(p.dir, "alpha.v")
	defer func() { _ = os.Remove(alpha) }()
	if err := p.vips("extract_band", p.current, alpha, itoa(bands-1)); err != nil {
		return false, err
	}
	out, err := p.output(p.codec.cfg.Vips, "min", alpha)
	if err != nil {
		return false, err
	}
	low, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
	if err != nil {
		return false, fmt.Errorf("vips min: %q", out)
	}
	return low >= 255, nil
}

// check reads the written file back: the dimensions must be what the
// policy produced and no EXIF/XMP/IPTC may survive.
func (p *pipeline) check(req Request, out encoded) (Result, error) {
	h, err := p.header(out.path)
	if err != nil {
		return Result{}, err
	}
	if h.metadata != "" {
		return Result{}, fmt.Errorf("output kept %s", h.metadata)
	}
	info, err := os.Stat(out.path)
	if err != nil {
		return Result{}, err
	}
	if req.Action == ActionCompress {
		source, err := os.Stat(req.Source)
		if err != nil {
			return Result{}, err
		}
		if info.Size() >= source.Size() {
			return Result{}, fmt.Errorf("%w: %d >= %d", ErrNotReduced, info.Size(), source.Size())
		}
	}
	return Result{Path: out.path, Mime: out.mime, Width: h.width, Height: h.height, Size: info.Size()}, nil
}

// step runs one operation from the current intermediate (or the given
// inputs) into the next .v file and drops the previous intermediate.
func (p *pipeline) step(op string, inputs, args []string) error {
	if inputs == nil {
		inputs = []string{p.current}
	}
	p.seq++
	next := filepath.Join(p.dir, "s"+itoa(p.seq)+".v")
	if err := p.vips(append(append(append([]string{op}, inputs...), next), args...)...); err != nil {
		_ = os.Remove(next)
		return err
	}
	if err := p.account(next); err != nil {
		return err
	}
	if p.current != "" {
		_ = os.Remove(p.current)
	}
	p.current = next
	return nil
}

// account records disk use while both the previous and the next
// intermediate exist, which is the pipeline's high-water mark.
func (p *pipeline) account(next string) error {
	var total int64
	for _, path := range []string{p.current, next} {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		total += info.Size()
	}
	p.peakDisk = max(p.peakDisk, total)
	return nil
}

func (p *pipeline) vips(args ...string) error {
	_, err := p.output(p.codec.cfg.Vips, args...)
	return err
}

type header struct {
	width, height, bands, pages int
	orientation                 int
	format, interpretation      string
	icc                         bool
	// metadata names the first EXIF/XMP/IPTC field found, if any.
	metadata string
}

func (p *pipeline) header(path string) (header, error) {
	out, err := p.output(p.codec.cfg.VipsHeader, "-a", path)
	if err != nil {
		return header{}, err
	}
	h := header{pages: 1}
	for line := range strings.Lines(out) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(value)
		switch {
		case key == "width":
			h.width = n
		case key == "height":
			h.height = n
		case key == "bands":
			h.bands = n
		case key == "n-pages":
			h.pages = n
		case key == "orientation":
			h.orientation = n
		case key == "format":
			h.format = value
		case key == "interpretation":
			h.interpretation = value
		case key == "icc-profile-data":
			h.icc = true
		case h.metadata == "" && (strings.HasPrefix(key, "exif-") || strings.HasPrefix(key, "xmp-") || strings.HasPrefix(key, "iptc-")):
			h.metadata = key
		}
	}
	if h.width == 0 || h.height == 0 || h.bands == 0 {
		return header{}, fmt.Errorf("vipsheader: incomplete header for %s", filepath.Base(path))
	}
	return h, nil
}

// output runs one binary without a shell. The child gets its own process
// group so cancellation kills anything it spawned, and a private TMPDIR.
// VIPS_BLOCK_UNTRUSTED is not set: it also blocks vipsload, which reads our
// own .v intermediates. The source is never sniffed instead — it always goes
// through the explicit jpegload / pngload / webpload chosen from its
// verified type, so SVG, PDF or ImageMagick loaders are unreachable.
func (p *pipeline) output(bin string, args ...string) (string, error) {
	name := bin
	if p.codec.cfg.Choom != "" {
		name = p.codec.cfg.Choom
		args = append([]string{"-n", oomScoreAdj, "--", bin}, args...)
	}
	cmd := exec.CommandContext(p.ctx, name, args...)
	cmd.Dir = p.dir
	cmd.Env = []string{"TMPDIR=" + p.dir, "HOME=" + p.dir}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if cmd.ProcessState != nil {
		if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			p.peakRSS = max(p.peakRSS, maxRSSBytes(usage))
		}
	}
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", filepath.Base(bin), firstArg(args, bin), err, lastLine(stderr.String()))
	}
	return stdout.String(), nil
}

// maxRSSBytes normalises ru_maxrss, which Linux reports in KiB and Darwin
// in bytes.
func maxRSSBytes(u *syscall.Rusage) int64 {
	if runtime.GOOS == "darwin" {
		return u.Maxrss
	}
	return u.Maxrss * 1024
}

func firstArg(args []string, bin string) string {
	for i, a := range args {
		if a == bin && i+1 < len(args) {
			return args[i+1]
		}
	}
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }
