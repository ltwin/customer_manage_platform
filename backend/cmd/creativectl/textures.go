package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
)

// The built-in textures (data: 内置贴图库) are imported from the catalog's
// list of files: textures import fetches what the object store lacks, taking
// the files whose source has no direct link from --dir; textures check only
// says what it lacks, for the production preflight.

// textureStore is the slice of creativescene.BuiltinTextures these commands
// drive, so their tests need no object store.
type textureStore interface {
	Import(context.Context, creativescene.FetchBuiltinTexture) creativescene.BuiltinImport
	Missing(context.Context) ([]creativescene.BuiltinTextureFile, error)
}

type texturesImportReport struct {
	envelope
	creativescene.BuiltinImport
}

type missingTexture struct {
	File     string `json:"file"`
	Material string `json:"material"`
}

type texturesCheckReport struct {
	envelope
	Missing []missingTexture `json:"missing"`
}

const (
	textureFetchTimeout = 10 * time.Minute
	textureFetchTries   = 3
)

func runTextures(ctx context.Context, verb string, args []string, deps commandDeps) int {
	operation := "textures " + verb
	flags := flag.NewFlagSet("creativectl-textures-"+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var dir string
	if verb == "import" {
		flags.StringVar(&dir, "dir", "", "")
	}
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		if verb == "import" {
			return fail(deps.stderr, operation, exitInvalidInput, operation+" 只接 --dir <目录>")
		}
		return fail(deps.stderr, operation, exitInvalidInput, operation+" 不接参数")
	}
	cfg, err := deps.loadConfig()
	if err != nil {
		return fail(deps.stderr, operation, exitInternal, "检查 creativectl 运行配置")
	}
	textures, err := deps.openTextures(cfg)
	if err != nil {
		return fail(deps.stderr, operation, exitInternal, "检查对象存储配置")
	}
	switch verb {
	case "import":
		r := textures.Import(ctx, fetchFromDir(dir, deps.fetchTexture))
		if len(r.Failed) > 0 {
			return report(deps.stdout, texturesImportReport{envelope{Operation: operation, Status: "incomplete", Remediation: "按 failed 的原因处理后重跑，已导入的文件会跳过"}, r}, exitInternal)
		}
		return report(deps.stdout, texturesImportReport{envelope{Operation: operation, Status: "imported"}, r}, exitOK)
	case "check":
		missing, err := textures.Missing(ctx)
		if err != nil {
			return fail(deps.stderr, operation, exitInternal, "读不了对象存储："+err.Error())
		}
		out := texturesCheckReport{envelope: envelope{Operation: operation, Status: "ready"}, Missing: []missingTexture{}}
		for _, f := range missing {
			out.Missing = append(out.Missing, missingTexture{File: f.Name, Material: f.Material})
		}
		if len(missing) > 0 {
			out.Status, out.Remediation = "missing", "运行 creativectl textures import"
			return report(deps.stdout, out, exitNotReady)
		}
		return report(deps.stdout, out, exitOK)
	}
	return fail(deps.stderr, "unknown", exitInvalidInput, usage)
}

func openTextures(cfg config.Config) (textureStore, error) {
	return creativescene.ComposeBuiltinTextures(cfg)
}

// fetchFromDir reads a file whose source has no direct link from dir, and
// fetches the rest from their sources.
func fetchFromDir(dir string, fetch creativescene.FetchBuiltinTexture) creativescene.FetchBuiltinTexture {
	return func(ctx context.Context, f creativescene.BuiltinTextureFile) ([]byte, error) {
		if f.URL != "" {
			return fetch(ctx, f)
		}
		if dir == "" {
			return nil, errors.New("来源站没有直链：用 --dir 指定放着这个文件的目录")
		}
		return readListed(dir, f.Name, f.Bytes)
	}
}

// readListed reads a file the catalog lists from dir; one byte past the
// listed size is enough to tell it does not match.
func readListed(dir, name string, size int64) ([]byte, error) {
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, size+1))
}

// fetchTexture downloads a file from its source, trying again when the
// connection breaks: the source sites drop connections now and then.
func fetchTexture(ctx context.Context, f creativescene.BuiltinTextureFile) ([]byte, error) {
	var err error
	for try := range textureFetchTries {
		if try > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(try) * 2 * time.Second):
			}
		}
		var body []byte
		if body, err = fetchOnce(ctx, f); err == nil {
			return body, nil
		}
	}
	return nil, err
}

func fetchOnce(ctx context.Context, f creativescene.BuiltinTextureFile) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, textureFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, err
	}
	// The source sites refuse a client that does not say what it is.
	req.Header.Set("User-Agent", "creativectl-textures/1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		// The request is read-only; ReadAll below reports truncated response bodies.
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// One byte past the listed size is enough to tell it does not match.
	return io.ReadAll(io.LimitReader(resp.Body, f.Bytes+1))
}
