// Command accountctl exposes trusted account-auth operations without exposing
// repository or migration primitives.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/authmail"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	exitOK           = 0
	exitInternal     = 1
	exitInvalidInput = 2
	exitNotReady     = 3
	exitDelivery     = 4

	accountctlPasswordEnv = "ACCOUNTCTL_AUTH_PASSWORD"
)

var runtimeBuildRevision = "development"

type authApplication interface {
	PlanBootstrap(context.Context, string, string) (auth.BootstrapPlan, error)
	Register(context.Context, string, string, auth.ClientMeta) (auth.DispatchResult, error)
	BeginLegacyClaim(context.Context, string, bool) (auth.LegacyClaimResult, error)
}

type applicationFactory func(
	context.Context,
	auth.RegistrationAdmissionMode,
	io.Writer,
) (authApplication, func(), error)

type commandDeps struct {
	stdin             io.Reader
	stdout            io.Writer
	stderr            io.Writer
	getenv            func(string) string
	openFile          func(string) (io.ReadCloser, error)
	now               func() time.Time
	newApplication    applicationFactory
	newReadinessProbe readinessProbeFactory
	buildRevision     string
}

type commandReport struct {
	Operation         string                    `json:"operation"`
	DryRun            bool                      `json:"dry_run"`
	Status            string                    `json:"status"`
	State             auth.LegacyClaimState     `json:"state,omitempty"`
	Eligible          *bool                     `json:"eligible,omitempty"`
	AccountRef        string                    `json:"account_ref,omitempty"`
	EmailRef          string                    `json:"email_ref,omitempty"`
	LegacyCount       int                       `json:"legacy_count,omitempty"`
	PendingClaimCount int                       `json:"pending_claim_count,omitempty"`
	FailureClass      auth.DeliveryFailureClass `json:"failure_class,omitempty"`
	Remediation       string                    `json:"remediation,omitempty"`
}

func main() {
	deps := commandDeps{
		stdin:             os.Stdin,
		stdout:            os.Stdout,
		stderr:            os.Stderr,
		getenv:            os.Getenv,
		openFile:          func(path string) (io.ReadCloser, error) { return os.Open(path) },
		now:               time.Now,
		newApplication:    newAuthApplication,
		newReadinessProbe: newStoreReadinessProbe,
		buildRevision:     runtimeBuildRevision,
	}
	os.Exit(run(context.Background(), os.Args[1:], deps))
}

func run(ctx context.Context, args []string, deps commandDeps) int {
	if deps.stdin == nil {
		deps.stdin = strings.NewReader("")
	}
	if deps.stdout == nil {
		deps.stdout = io.Discard
	}
	if deps.stderr == nil {
		deps.stderr = io.Discard
	}
	if deps.getenv == nil {
		deps.getenv = os.Getenv
	}
	if deps.openFile == nil {
		deps.openFile = func(path string) (io.ReadCloser, error) { return os.Open(path) }
	}
	if deps.now == nil {
		deps.now = time.Now
	}
	if deps.newApplication == nil {
		deps.newApplication = newAuthApplication
	}
	if deps.newReadinessProbe == nil {
		deps.newReadinessProbe = newStoreReadinessProbe
	}
	if deps.buildRevision == "" {
		deps.buildRevision = runtimeBuildRevision
	}
	if len(args) < 2 || args[0] != "auth" {
		return writeFailure(deps.stderr, "unknown", exitInvalidInput, "invalid_input", "使用 auth bootstrap、auth claim-legacy、auth monitor 或 auth readiness")
	}
	switch args[1] {
	case "bootstrap":
		return runBootstrap(ctx, args[2:], deps)
	case "claim-legacy":
		return runLegacyClaim(ctx, args[2:], deps)
	case "monitor":
		return runAuthMonitor(ctx, args[2:], deps)
	case "readiness":
		return runAuthReadiness(ctx, args[2:], deps)
	default:
		return writeFailure(deps.stderr, "unknown", exitInvalidInput, "invalid_input", "使用 auth bootstrap、auth claim-legacy、auth monitor 或 auth readiness")
	}
}

func runBootstrap(ctx context.Context, args []string, deps commandDeps) int {
	email, dryRun, ok := parseEmailFlags(args)
	if !ok {
		return writeFailure(deps.stderr, "bootstrap", exitInvalidInput, "invalid_input", "只允许 --email 与可选 --dry-run；密码不得通过 argv")
	}
	password := deps.getenv(accountctlPasswordEnv)
	if password == "" {
		return writeFailure(deps.stderr, "bootstrap", exitInvalidInput, "invalid_input", "通过 ACCOUNTCTL_AUTH_PASSWORD 注入密码")
	}
	application, closeApplication, err := deps.newApplication(ctx, auth.RegistrationBootstrapFirstAccount, deps.stderr)
	if err != nil {
		return writeFailure(deps.stderr, "bootstrap", exitInternal, "internal", "检查 accountctl 运行配置")
	}
	defer closeApplication()

	if dryRun {
		plan, err := application.PlanBootstrap(ctx, email, password)
		if err != nil {
			return writeAuthFailure(deps.stderr, "bootstrap", err)
		}
		status := "ready"
		exitCode := exitOK
		remediation := ""
		if !plan.Eligible {
			status = "not_ready"
			exitCode = exitNotReady
			remediation = "改走公开注册或 legacy claim"
		}
		report := commandReport{
			Operation: "bootstrap", DryRun: true, Status: status,
			Eligible: boolReference(plan.Eligible), EmailRef: plan.RecipientRef,
			Remediation: remediation,
		}
		return writeReport(deps.stdout, report, exitCode)
	}

	dispatch, err := application.Register(ctx, email, password, auth.ClientMeta{SourceIP: netip.MustParseAddr("127.0.0.1")})
	if err != nil {
		return writeAuthFailure(deps.stderr, "bootstrap", err)
	}
	report := commandReport{
		Operation: "bootstrap", DryRun: false, Status: deliveryStatus(dispatch.Delivery),
		FailureClass: dispatch.Delivery.FailureClass,
	}
	if !dispatch.Attempted {
		report.Status = "not_ready"
		report.Remediation = "改走公开注册或 legacy claim"
		return writeReport(deps.stdout, report, exitNotReady)
	}
	if !dispatch.Delivery.Accepted {
		report.Remediation = "检查邮件 provider 后调用 /auth/email/resend；不要重跑 bootstrap"
		return writeReport(deps.stdout, report, exitDelivery)
	}
	return writeReport(deps.stdout, report, exitOK)
}

func runLegacyClaim(ctx context.Context, args []string, deps commandDeps) int {
	email, dryRun, ok := parseEmailFlags(args)
	if !ok {
		return writeFailure(deps.stderr, "claim-legacy", exitInvalidInput, "invalid_input", "只允许 --email 与可选 --dry-run")
	}
	application, closeApplication, err := deps.newApplication(ctx, auth.RegistrationPublic, deps.stderr)
	if err != nil {
		if writeLegacyClaimEvent(deps.stderr, deps.now(), dryRun, auth.LegacyClaimResult{}, err) != nil {
			return exitInternal
		}
		return writeFailure(deps.stderr, "claim-legacy", exitInternal, "internal", "检查 accountctl 运行配置")
	}
	defer closeApplication()

	result, err := application.BeginLegacyClaim(ctx, email, dryRun)
	if err != nil {
		if writeLegacyClaimEvent(deps.stderr, deps.now(), dryRun, result, err) != nil {
			return exitInternal
		}
		return writeAuthFailure(deps.stderr, "claim-legacy", err)
	}
	if writeLegacyClaimEvent(deps.stderr, deps.now(), dryRun, result, nil) != nil {
		return exitInternal
	}
	report := commandReport{
		Operation: "claim-legacy", DryRun: dryRun,
		Status: deliveryStatus(result.Delivery), State: result.State,
		AccountRef: result.AccountIDRedacted, EmailRef: result.EmailRedacted,
		LegacyCount: result.LegacyCount, PendingClaimCount: result.PendingClaimCount,
		FailureClass: result.Delivery.FailureClass,
	}
	if result.State != auth.LegacyClaimReady && result.State != auth.LegacyClaimPendingSameEmail {
		report.Status = "not_ready"
		report.Remediation = legacyRemediation(result.State)
		return writeReport(deps.stdout, report, exitNotReady)
	}
	if dryRun {
		report.Status = "ready"
		return writeReport(deps.stdout, report, exitOK)
	}
	if !result.Delivery.Accepted {
		report.Remediation = "检查邮件 provider 后重跑相同 claim"
		return writeReport(deps.stdout, report, exitDelivery)
	}
	return writeReport(deps.stdout, report, exitOK)
}

func parseEmailFlags(args []string) (string, bool, bool) {
	flags := flag.NewFlagSet("accountctl-auth", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	email := flags.String("email", "", "")
	dryRun := flags.Bool("dry-run", false, "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*email) == "" {
		return "", false, false
	}
	return *email, *dryRun, true
}

func writeAuthFailure(output io.Writer, operation string, err error) int {
	var classified interface{ Kind() auth.AuthErrorKind }
	if errors.As(err, &classified) {
		switch classified.Kind() {
		case auth.AuthErrorValidation:
			return writeFailure(output, operation, exitInvalidInput, "invalid_input", "检查输入格式")
		case auth.AuthErrorBootstrapNotEmpty:
			return writeFailure(output, operation, exitNotReady, "bootstrap_not_empty", "改走公开注册或 legacy claim")
		}
	}
	return writeFailure(output, operation, exitInternal, "internal", "检查 accountctl 运行配置")
}

func writeFailure(output io.Writer, operation string, exitCode int, status, remediation string) int {
	return writeReport(output, commandReport{
		Operation: operation, Status: status, Remediation: remediation,
	}, exitCode)
}

func writeReport(output io.Writer, report commandReport, exitCode int) int {
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return exitInternal
	}
	return exitCode
}

func deliveryStatus(outcome auth.DeliveryOutcome) string {
	switch {
	case outcome.Accepted:
		return "accepted"
	case outcome.Attempted:
		return "failed"
	default:
		return "not_attempted"
	}
}

func legacyRemediation(state auth.LegacyClaimState) string {
	switch state {
	case auth.LegacyClaimAlreadyClaimed:
		return "账号已完成 legacy claim"
	case auth.LegacyClaimConflict:
		return "消除 legacy 目标或邮箱冲突后重试"
	default:
		return "确认存在且仅存在一个 legacy_unclaimed 账号"
	}
}

func boolReference(value bool) *bool { return &value }

func newAuthApplication(
	ctx context.Context,
	mode auth.RegistrationAdmissionMode,
	logOutput io.Writer,
) (authApplication, func(), error) {
	cfg, err := config.LoadAccountAuth()
	if err != nil {
		return nil, nil, err
	}
	database, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))
	options := []auth.ServiceOption{
		auth.WithPublicBaseURL(cfg.PublicBaseURL),
		auth.WithAttemptLimiter(database),
		auth.WithRegistrationAdmissionMode(mode),
	}
	switch cfg.AuthMailDriver {
	case "sink":
		options = append(options, auth.WithAuthMailSender(authmail.NewSink(logger, nil)))
	case "resend":
		options = append(options, auth.WithAuthMailSender(
			authmail.NewResend(cfg.ResendAPIKey, cfg.AuthMailFrom, logger),
		))
	}
	issuer := auth.NewTokenIssuer(cfg.AuthTokenSecret).WithIdentity(cfg.AuthTokenIssuer, "photographer-crm-web")
	return auth.NewService(database, issuer, options...), database.Close, nil
}
