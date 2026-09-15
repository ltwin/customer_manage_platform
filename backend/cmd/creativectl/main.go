// Command creativectl is the trusted administrative entry point for creative
// skills. Publishing is a deployment act, not a photographer's request: there
// is no HTTP surface for it, and this command is the only way a skill version
// comes into existence or its objects are reclaimed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	exitOK           = 0
	exitInternal     = 1
	exitInvalidInput = 2
	exitNotReady     = 3
	// A conflict is somebody else's correct work, not this caller's mistake: a
	// deploy script has to be able to tell the two apart without parsing text.
	exitConflict = 4

	usage = "使用 skill import 或 skill reclaim"
)

// application is the slice of the skill service this command drives. Narrowing
// it here keeps the command's own tests free of a database.
type application interface {
	Publish(context.Context, store.AccountScope, creativeskill.Package, creativeskill.PublishIntent) (creativeskill.Version, error)
	ReclaimExpiredImports(context.Context, store.AccountScope, int) (creativeskill.Reclaimed, error)
}

type commandDeps struct {
	stdout     io.Writer
	stderr     io.Writer
	loadConfig func() (config.Config, error)
	open       func(context.Context, config.Config, string) (application, store.AccountScope, func(), error)
}

// envelope is what every invocation reports, success or not.
type envelope struct {
	Operation   string `json:"operation"`
	Status      string `json:"status"`
	Remediation string `json:"remediation,omitempty"`
}

type importReport struct {
	envelope
	SkillID   string `json:"skill_id"`
	VersionID string `json:"version_id"`
	// SkillRevision is what the next import of this skill must pass as
	// --expected-revision. Leaving it out would make appending a version
	// require a database query, and version_number is not a substitute: the
	// revision moves on every change to the skill, not only on a new version.
	SkillRevision int64  `json:"skill_revision"`
	VersionNumber int    `json:"version_number"`
	Digest        string `json:"digest"`
}

// The sweep counts are never omitted: "nothing was due" and "nothing could be
// deleted" must not both render as a missing field.
type reclaimReport struct {
	envelope
	Imports int `json:"imports"`
	Objects int `json:"objects"`
	Failed  int `json:"failed"`
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], commandDeps{
		stdout: os.Stdout, stderr: os.Stderr,
		loadConfig: config.Load, open: openStore,
	}))
}

func run(ctx context.Context, args []string, deps commandDeps) int {
	if deps.stdout == nil {
		deps.stdout = io.Discard
	}
	if deps.stderr == nil {
		deps.stderr = io.Discard
	}
	if deps.loadConfig == nil {
		deps.loadConfig = config.Load
	}
	if deps.open == nil {
		deps.open = openStore
	}
	if len(args) < 2 || args[0] != "skill" {
		return fail(deps.stderr, "unknown", exitInvalidInput, usage)
	}
	switch args[1] {
	case "import":
		return runImport(ctx, args[2:], deps)
	case "reclaim":
		return runReclaim(ctx, args[2:], deps)
	default:
		return fail(deps.stderr, "unknown", exitInvalidInput, usage)
	}
}

type importFlags struct {
	packageDir  string
	account     string
	operationID string
	origin      string
	expected    int64
	activate    bool
}

func runImport(ctx context.Context, args []string, deps commandDeps) int {
	flags := flag.NewFlagSet("creativectl-skill-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var in importFlags
	flags.StringVar(&in.packageDir, "package-dir", "", "")
	flags.StringVar(&in.account, "account", "", "")
	flags.StringVar(&in.operationID, "operation-id", "", "")
	flags.StringVar(&in.origin, "origin", "account", "")
	flags.Int64Var(&in.expected, "expected-revision", 0, "")
	flags.BoolVar(&in.activate, "activate", false, "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return fail(deps.stderr, "import", exitInvalidInput, "只允许 --package-dir --account --operation-id --origin --expected-revision --activate")
	}
	in.packageDir = strings.TrimSpace(in.packageDir)
	in.account = strings.TrimSpace(in.account)
	in.operationID = strings.TrimSpace(in.operationID)
	if in.packageDir == "" || in.account == "" {
		return fail(deps.stderr, "import", exitInvalidInput, "--package-dir 与 --account 必填")
	}
	// The operation id is required rather than generated, because it is what
	// makes rerunning this command a replay instead of a second publication.
	// A deploy script that reruns the same id gets the version it already made.
	if !creativeops.ValidOperationID(in.operationID) {
		return fail(deps.stderr, "import", exitInvalidInput, "--operation-id 必须是 UUID；重跑同一个 id 即为重放")
	}
	if in.expected < 0 {
		return fail(deps.stderr, "import", exitInvalidInput, "--expected-revision 不能为负；新建 Skill 时留空")
	}
	cfg, err := deps.loadConfig()
	if err != nil {
		return fail(deps.stderr, "import", exitInternal, "检查 creativectl 运行配置")
	}
	switch in.origin {
	case "account":
	case "platform":
		// Which account owns the platform catalog is a deployment fact. Letting
		// the flag decide would make a typo enough to mint a platform skill that
		// every account can see.
		if cfg.CreativePlatformAccountID == "" {
			return fail(deps.stderr, "import", exitNotReady, "未配置 CREATIVE_PLATFORM_ACCOUNT_ID：本部署没有平台发布账号")
		}
		if cfg.CreativePlatformAccountID != in.account {
			return fail(deps.stderr, "import", exitInvalidInput, "--account 必须是配置里的平台发布账号")
		}
	default:
		return fail(deps.stderr, "import", exitInvalidInput, "--origin 只能是 account 或 platform")
	}
	pkg, err := creativeskill.ReadPackage(os.DirFS(in.packageDir))
	if err != nil {
		return fail(deps.stderr, "import", exitInvalidInput, "包目录不合法："+err.Error())
	}
	app, scope, closeApp, err := deps.open(ctx, cfg, in.account)
	if err != nil {
		return fail(deps.stderr, "import", exitInternal, "检查 creativectl 运行配置")
	}
	defer closeApp()

	version, err := app.Publish(ctx, scope, pkg, creativeskill.PublishIntent{
		OperationID: in.operationID, Origin: in.origin,
		ExpectedSkillRevision: creativeops.Revision(in.expected), Activate: in.activate,
	})
	if err != nil {
		return failSkill(deps.stderr, "import", err)
	}
	return report(deps.stdout, importReport{
		envelope: envelope{Operation: "import", Status: "published"},
		SkillID:  version.SkillID, VersionID: version.ID,
		SkillRevision: int64(version.SkillRevision),
		VersionNumber: version.VersionNumber, Digest: version.Digest,
	}, exitOK)
}

func runReclaim(ctx context.Context, args []string, deps commandDeps) int {
	flags := flag.NewFlagSet("creativectl-skill-reclaim", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	account := flags.String("account", "", "")
	limit := flags.Int("limit", 50, "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*account) == "" || *limit < 1 {
		return fail(deps.stderr, "reclaim", exitInvalidInput, "只允许 --account（必填）与 --limit（正整数）")
	}
	cfg, err := deps.loadConfig()
	if err != nil {
		return fail(deps.stderr, "reclaim", exitInternal, "检查 creativectl 运行配置")
	}
	app, scope, closeApp, err := deps.open(ctx, cfg, strings.TrimSpace(*account))
	if err != nil {
		return fail(deps.stderr, "reclaim", exitInternal, "检查 creativectl 运行配置")
	}
	defer closeApp()

	// Nothing sweeps expired imports on a timer in this phase; running this
	// command is the whole reclamation. Frozen versions are never touched.
	reclaimed, sweepErr := app.ReclaimExpiredImports(ctx, scope, *limit)
	result := reclaimReport{
		envelope: envelope{Operation: "reclaim", Status: "swept"},
		Imports:  reclaimed.Imports, Objects: reclaimed.Objects, Failed: reclaimed.Failed,
	}
	if sweepErr != nil {
		// A partial sweep still made progress and still has work left. Reporting
		// it as a flat failure would hide both halves.
		result.Status = "partial"
		result.Remediation = "部分导入未能回收，下次 sweep 会重试：" + sweepErr.Error()
		return report(deps.stdout, result, exitNotReady)
	}
	return report(deps.stdout, result, exitOK)
}

func failSkill(output io.Writer, operation string, err error) int {
	switch {
	case errors.Is(err, creativeskill.ErrImportConflict):
		return fail(output, operation, exitConflict, "同一 operation id 已用于另一份内容：换一个 id")
	case errors.Is(err, creativeskill.ErrRevisionConflict):
		return fail(output, operation, exitConflict, "Skill 已被其他发布移动：重新读取 revision 后重试")
	case errors.Is(err, creativeskill.ErrSkillIntent):
		// Not a conflict: retrying this exact invocation can never succeed.
		return fail(output, operation, exitInvalidInput,
			"--expected-revision 与 Skill 现状不符：新建 Skill 时留空，追加版本时填上一次返回的 revision")
	case errors.Is(err, creativeskill.ErrOriginNotPermitted):
		return fail(output, operation, exitInvalidInput, "该账号不是配置里的平台发布账号")
	case errors.Is(err, creativeskill.ErrImportExpired):
		return fail(output, operation, exitNotReady, "导入已过窗口：用新的 operation id 重跑")
	case errors.Is(err, creativeskill.ErrContentMismatch):
		return fail(output, operation, exitInvalidInput, "包内文件与声明不一致：确认目录未在导入过程中被改动")
	case errors.Is(err, creativeops.ErrValidation), errors.Is(err, creativeskill.ErrLimit):
		return fail(output, operation, exitInvalidInput, "包内容不合法或超出上限："+err.Error())
	case errors.Is(err, creativeskill.ErrResourceUnavailable):
		return fail(output, operation, exitNotReady, "对象存储不可用：确认驱动配置后重跑同一个 operation id")
	default:
		return fail(output, operation, exitInternal, "检查 creativectl 运行配置与数据库连接")
	}
}

func fail(output io.Writer, operation string, exitCode int, remediation string) int {
	return report(output, envelope{
		Operation: operation, Status: "failed", Remediation: remediation,
	}, exitCode)
}

func report(output io.Writer, r any, exitCode int) int {
	if err := json.NewEncoder(output).Encode(r); err != nil {
		return exitInternal
	}
	return exitCode
}

func openStore(ctx context.Context, cfg config.Config, accountID string) (application, store.AccountScope, func(), error) {
	database, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, store.AccountScope{}, nil, err
	}
	svc, err := creativeskill.Compose(cfg)
	if err != nil {
		database.Close()
		return nil, store.AccountScope{}, nil, err
	}
	return svc, database.ScopeFor(auth.AccountContext{AccountID: accountID}), database.Close, nil
}
