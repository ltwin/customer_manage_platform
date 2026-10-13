package creativeimageedit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SelfCheck proves at startup that this image can do what the policy
// promises: every loader and saver, a profile conversion and, when
// configured, the OOM score on the child. A failure must stop the worker
// from offering image editing rather than surface on the first request.
func (c *Codec) SelfCheck(ctx context.Context) (string, error) {
	dir, err := os.MkdirTemp(c.cfg.TempRoot, tempPrefix)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	p := &pipeline{codec: c, ctx: ctx, dir: dir}
	version, err := p.output(c.cfg.Vips, "--version")
	if err != nil {
		return "", err
	}
	version = strings.TrimSpace(version)
	if c.cfg.Choom != "" {
		cat, err := exec.LookPath("cat")
		if err != nil {
			return "", err
		}
		score, err := p.output(cat, "/proc/self/oom_score_adj")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(score) != oomScoreAdj {
			return "", fmt.Errorf("child oom_score_adj is %q, want %s", strings.TrimSpace(score), oomScoreAdj)
		}
	}
	base := filepath.Join(dir, "base.v")
	if err := p.vips("black", filepath.Join(dir, "black.v"), "16", "16", "--bands", "3"); err != nil {
		return "", err
	}
	if err := p.vips("linear", filepath.Join(dir, "black.v"), base, "1 1 1", "200 40 20", "--uchar"); err != nil {
		return "", err
	}
	sources := map[string]string{
		"image/jpeg": filepath.Join(dir, "p3.jpg"),
		"image/png":  filepath.Join(dir, "plain.png"),
		"image/webp": filepath.Join(dir, "plain.webp"),
	}
	if err := p.vips("icc_transform", base, sources["image/jpeg"], "p3", "--input-profile", "srgb"); err != nil {
		return "", err
	}
	if err := p.vips("pngsave", base, sources["image/png"]); err != nil {
		return "", err
	}
	if err := p.vips("webpsave", base, sources["image/webp"]); err != nil {
		return "", err
	}
	for mime, source := range sources {
		result, err := c.Render(ctx, Request{Source: source, Mime: mime, Action: ActionCrop, Crop: Crop{Left: 2, Top: 2, Width: 8, Height: 8}})
		if err != nil {
			return "", fmt.Errorf("render %s: %w", mime, err)
		}
		_ = result.Close()
		if result.Mime != mime || result.Width != 8 || result.Height != 8 {
			return "", fmt.Errorf("render %s: got %s %dx%d", mime, result.Mime, result.Width, result.Height)
		}
	}
	return version, nil
}
