package main

import (
	"context"
	"flag"
	"io"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
)

// The built-in models (data: 内置模型库) are imported from a directory the
// conversion script wrote, as their sources have no direct links: models
// import --dir writes what the object store lacks once it matches the
// catalog, models check only says what it lacks.

// modelStore is the slice of creativescene.BuiltinModels these commands
// drive, so their tests need no object store.
type modelStore interface {
	Import(context.Context, creativescene.ReadBuiltinModel) creativescene.BuiltinModelImport
	Missing(context.Context) ([]creativescene.BuiltinModelFile, error)
}

type modelsImportReport struct {
	envelope
	creativescene.BuiltinModelImport
}

type missingModel struct {
	File  string `json:"file"`
	Model string `json:"model"`
}

type modelsCheckReport struct {
	envelope
	Missing []missingModel `json:"missing"`
}

func runModels(ctx context.Context, verb string, args []string, deps commandDeps) int {
	operation := "models " + verb
	flags := flag.NewFlagSet("creativectl-models-"+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var dir string
	if verb == "import" {
		flags.StringVar(&dir, "dir", "", "")
	}
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || verb == "import" && dir == "" {
		if verb == "import" {
			return fail(deps.stderr, operation, exitInvalidInput, operation+" 须用 --dir <目录> 指定转换脚本写出的目录")
		}
		return fail(deps.stderr, operation, exitInvalidInput, operation+" 不接参数")
	}
	cfg, err := deps.loadConfig()
	if err != nil {
		return fail(deps.stderr, operation, exitInternal, "检查 creativectl 运行配置")
	}
	models, err := deps.openModels(cfg)
	if err != nil {
		return fail(deps.stderr, operation, exitInternal, "检查对象存储配置")
	}
	switch verb {
	case "import":
		r := models.Import(ctx, func(_ context.Context, f creativescene.BuiltinModelFile) ([]byte, error) {
			return readListed(dir, f.Name, f.Bytes)
		})
		if len(r.Failed) > 0 {
			return report(deps.stdout, modelsImportReport{envelope{Operation: operation, Status: "incomplete", Remediation: "按 failed 的原因处理后重跑，已导入的文件会跳过"}, r}, exitInternal)
		}
		return report(deps.stdout, modelsImportReport{envelope{Operation: operation, Status: "imported"}, r}, exitOK)
	case "check":
		missing, err := models.Missing(ctx)
		if err != nil {
			return fail(deps.stderr, operation, exitInternal, "读不了对象存储："+err.Error())
		}
		out := modelsCheckReport{envelope: envelope{Operation: operation, Status: "ready"}, Missing: []missingModel{}}
		for _, f := range missing {
			out.Missing = append(out.Missing, missingModel{File: f.Name, Model: f.Model})
		}
		if len(missing) > 0 {
			out.Status, out.Remediation = "missing", "运行 creativectl models import --dir <目录>"
			return report(deps.stdout, out, exitNotReady)
		}
		return report(deps.stdout, out, exitOK)
	}
	return fail(deps.stderr, "unknown", exitInvalidInput, usage)
}

func openModels(cfg config.Config) (modelStore, error) {
	return creativescene.ComposeBuiltinModels(cfg)
}
