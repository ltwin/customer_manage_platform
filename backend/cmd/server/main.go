// Command server 是 CRM 单体后端入口。
// 启动序（design 2.2）：加载 env 配置 → migrate up → HTTP 监听。
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/accountprofile"
	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarimage"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarstore"
	"github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/dataexport"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/authmail"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	telegramapi "github.com/samson/customer-manage-platform/backend/internal/reminder/digest/telegram"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	planningbusiness "github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/ingestion"
)

const telegramAPIBaseURL = "https://api.telegram.org"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logStartupFailure(logger, err)
		os.Exit(1)
	}
}

type startupFailure struct {
	operation  string
	configKey  string
	errorClass string
	cause      error
}

func newStartupFailure(operation, configKey, errorClass string, cause error) error {
	return &startupFailure{
		operation:  operation,
		configKey:  configKey,
		errorClass: errorClass,
		cause:      cause,
	}
}

func (e *startupFailure) Error() string { return "application startup failed" }

func (e *startupFailure) Unwrap() error { return e.cause }

func logStartupFailure(logger *slog.Logger, err error) {
	failure := &startupFailure{
		operation:  "application-start",
		configKey:  "none",
		errorClass: "internal",
	}
	var classified *startupFailure
	if errors.As(err, &classified) {
		failure = classified
	}
	logger.Error(
		"startup failed",
		slog.String("operation", failure.operation),
		slog.String("config_key", failure.configKey),
		slog.String("error_class", failure.errorClass),
	)
}

func classifyConfigStartupFailure(err error) error {
	key := "CONFIGURATION"
	errorClass := "invalid_config"
	switch {
	case errors.Is(err, config.ErrDatabaseURLMissing):
		key = "DATABASE_URL"
	case errors.Is(err, config.ErrAuthTokenSecretMissing):
		key = "AUTH_TOKEN_SECRET"
	case errors.Is(err, config.ErrPublicBaseURLMissing), errors.Is(err, config.ErrPublicBaseURLInvalid):
		key = "PUBLIC_BASE_URL"
	case errors.Is(err, config.ErrAuthPublicRegistrationInvalid):
		key = "AUTH_PUBLIC_REGISTRATION_ENABLED"
	case errors.Is(err, config.ErrAuthMailDriverInvalid):
		key = "AUTH_MAIL_DRIVER"
	case errors.Is(err, config.ErrResendAPIKeyMissing):
		key = "RESEND_API_KEY"
	case errors.Is(err, config.ErrTrustedProxyCIDRsInvalid):
		key = "TRUSTED_PROXY_CIDRS"
	case errors.Is(err, config.ErrAvatarStorageDriverInvalid):
		key = "AVATAR_STORAGE_DRIVER"
	case errors.Is(err, config.ErrAvatarLocalRootMissing),
		errors.Is(err, config.ErrAvatarLocalRootNotMount),
		errors.Is(err, config.ErrAvatarLocalRootUnavailable):
		key = "AVATAR_LOCAL_ROOT"
		if errors.Is(err, config.ErrAvatarLocalRootUnavailable) {
			errorClass = "filesystem"
		}
	case errors.Is(err, config.ErrAvatarLocalRequireMountInvalid):
		key = "AVATAR_LOCAL_REQUIRE_MOUNT"
	}
	return newStartupFailure("config-load", key, errorClass, err)
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return classifyConfigStartupFailure(err)
	}

	if err := store.MigrateUp(cfg.DatabaseURL); err != nil {
		return newStartupFailure("database-migrate", "DATABASE_URL", "database", err)
	}

	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return newStartupFailure("database-open", "DATABASE_URL", "database", err)
	}
	defer s.Close()
	idempotencyExecutor := idempotency.NewExecutor()
	planningMediaObjects, err := immutablefs.NewLocal(filepath.Join(filepath.Dir(cfg.AvatarLocalRoot), "planning-media"))
	if err != nil {
		return newStartupFailure("planning-media-store-init", "AVATAR_LOCAL_ROOT", "filesystem", err)
	}
	planningMediaApp := planningmedia.NewApplication(
		planningmedia.Repository{},
		idempotencyExecutor,
		planningMediaObjects,
		planningmedia.WithHolderAuthorizer(shootplanning.NewMediaHolderAuthorizer(shootplanning.NewPostgresRepository())),
	)
	ingestionRepo := ingestion.NewRepository()
	shootPlanningApp, err := composeShootPlanningApplicationWithIngestion(
		ctx,
		s.ArchiveCapabilityStartupReader(),
		idempotencyExecutor,
		planningMediaApp,
		ingestion.NewPlanReadyObservationAdapter(ingestionRepo),
	)
	if err != nil {
		return err
	}
	shootPlanningBusiness, err := planningbusiness.NewApplication(
		planningbusiness.NewRepository(),
		idempotencyExecutor,
		order.NewBusinessAdjustmentParticipant(),
		schedule.NewBusinessDurationParticipant(shootPlanningApp.CRM()),
	)
	if err != nil {
		return newStartupFailure("shoot-planning-business-wiring", "PLANNING_BUSINESS", "invalid_config", err)
	}
	planningIngestionApp := ingestion.NewApplication(ingestionRepo, idempotencyExecutor,
		ingestion.WithCoreApplication(shootPlanningApp), ingestion.WithPlanningMediaApplication(planningMediaApp))
	planShareApp := planshare.NewApplication(idempotencyExecutor)
	planShareResolver := planshare.NewResolver(
		planshare.StoreTokenLookup{Store: s},
		planshare.DefaultTrustedCapabilityFactory{},
	)
	planShareRunner := planshare.StoreShareTransactionRunner{Store: s, Media: planningMediaApp}
	planShareIPDigest := planshare.FailClosedIPDigestResolver{
		Key: derivePlanshareIPDigestKey(cfg.AuthTokenSecret),
	}
	planShareReadBudget := planshare.SecurityBudgetReadGate{
		Gate: store.SecurityAttemptBudgetGate{Store: s},
	}
	planShareMutationDigest := planshare.FailClosedMutationIPDigestResolver{
		Key: derivePlanshareIPDigestKey(cfg.AuthTokenSecret),
	}
	planShareMutationBudget := planshare.SecurityBudgetMutationGate{
		Gate: store.SecurityAttemptBudgetGate{Store: s},
	}

	objects, err := avatarstore.NewLocal(cfg.AvatarLocalRoot)
	if err != nil {
		return newStartupFailure("avatar-store-init", "AVATAR_LOCAL_ROOT", "filesystem", err)
	}
	avatarRepo := customer.NewPostgresAvatarRepository()
	avatarApp := customer.NewAvatarApplication(avatarRepo, objects)
	maintenance := customer.NewAvatarMaintenanceRunner(s, avatarRepo, objects, logger)
	profileRepo := accountprofile.NewPostgresRepository()
	profileSvc := accountprofile.NewService(profileRepo, objects)
	profileMaintenance := accountprofile.NewMaintenanceRunner(s, profileRepo, objects, logger)

	settingsSvc := settings.NewService(settings.NewPostgresRepository()).WithScopeFactory(func(accountID string) store.AccountScope {
		return s.ScopeFor(auth.AccountContext{AccountID: accountID})
	})
	var settingsErr error
	settingsSvc, settingsErr = settingsSvc.WithPlanningReminderTimezone(
		true,
		reminder.NewTimezoneChangePlanningParticipant(nil, nil),
	)
	if settingsErr != nil {
		return newStartupFailure("settings-timezone-wiring", "SETTINGS", "invalid_config", settingsErr)
	}
	assignmentFreshness := reminder.NewAssignmentReminderFreshness()
	reminderSvc := reminder.NewService(
		reminder.NewPostgresRepository(),
		reminder.NewSettingsAdapter(settingsSvc),
		logger,
	).WithFreshness(assignmentFreshness)
	reminderRunner := reminder.NewScanRunner(s, reminderSvc, settingsSvc, logger)
	assignmentReminderRunner := reminder.NewPlanningAssignmentRunner(s, logger)
	dashboardSvc := dashboard.NewService(dashboard.NewPostgresRepository(), settingsSvc)
	dataExportSvc := dataexport.NewService(dataexport.NewPostgresRepository(), dataexport.ClockFunc(time.Now))
	telegramBinding, telegramRunner, telegramErr := buildTelegramIntegration(
		cfg,
		s,
		settingsSvc,
		reminderSvc,
		assignmentFreshness,
		logger,
	)
	if telegramErr != nil {
		logger.Warn(
			"telegram integration unavailable",
			slog.String("status", "invalid_config"),
			slog.String("config_key", "TELEGRAM_BOT_TOKEN/TELEGRAM_BOT_USERNAME"),
			slog.String("error_class", "invalid_config"),
		)
	} else if telegramRunner == nil {
		logger.Info("telegram integration disabled", slog.String("status", "disabled"))
	} else {
		logger.Info("telegram integration enabled", slog.String("status", "active"))
	}

	tokenIssuer := auth.NewTokenIssuer(cfg.AuthTokenSecret).WithIdentity(cfg.AuthTokenIssuer, "photographer-crm-web")
	authOptions := []auth.ServiceOption{auth.WithPublicBaseURL(cfg.PublicBaseURL), auth.WithAttemptLimiter(s)}
	switch cfg.AuthMailDriver {
	case "sink":
		authOptions = append(authOptions, auth.WithAuthMailSender(authmail.NewSink(logger, time.Now)))
	case "resend":
		authOptions = append(authOptions, auth.WithAuthMailSender(
			authmail.NewResend(cfg.ResendAPIKey, cfg.AuthMailFrom, logger),
		))
	}
	authService := auth.NewService(s, tokenIssuer, authOptions...)
	authReplayRunner := newAuthReplaySweepRunner(authService, logger)
	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:                    logger,
		DB:                        s,
		ScopeFactory:              s,
		Auth:                      authService,
		PublicBaseURL:             cfg.PublicBaseURL,
		PublicRegistrationEnabled: cfg.AuthPublicRegistrationEnabled,
		TrustedProxyCIDRs:         cfg.TrustedProxyPrefixes,
		Customer:                  customer.NewService(customer.NewPostgresRepository().WithPlanMergeParticipant(shootPlanningApp.CRM())),
		Orders: order.NewService(
			order.NewPostgresRepository().WithLifecycleParticipant(shootPlanningApp.CRM()),
		).WithDeliveryPolicyProvider(order.NewSettingsDeliveryPolicyAdapter(settingsSvc)),
		Packages:              pkgcatalog.NewService(pkgcatalog.NewPostgresRepository()),
		Idempotency:           idempotencyExecutor,
		AccountTimezone:       settingsSvc,
		Schedule:              schedule.NewService(schedule.NewPostgresRepository().WithProjectionSink(shootPlanningApp.CRM()), schedule.ClockFunc(time.Now)),
		Avatar:                avatarApp,
		AvatarProcessor:       avatarimage.NewProcessor(),
		AccountProfile:        profileSvc,
		Settings:              settingsSvc,
		Reminders:             reminderSvc,
		Dashboard:             dashboardSvc,
		DataExport:            dataExportSvc,
		TelegramBinding:       telegramBinding,
		ShootPlanning:         shootPlanningApp,
		ShootPlanningBusiness: shootPlanningBusiness,
		PlanShare:             planShareApp,
		AnonymousShare: httpapi.AnonymousShareDeps{
			App:            planShareApp,
			Resolver:       planShareResolver,
			Runner:         planShareRunner,
			PlanningMedia:  planningMediaApp,
			ReadBudget:     planShareReadBudget,
			IPDigest:       planShareIPDigest,
			MutationBudget: planShareMutationBudget,
			MutationDigest: planShareMutationDigest,
			PublicBaseURL:  cfg.PublicBaseURL,
		},
		PlanningMedia:     planningMediaApp,
		PlanningIngestion: planningIngestionApp,
	})

	logger.Info("HTTP 监听", slog.String("addr", cfg.HTTPAddr))
	// 公网直挂无反代（design D6），ReadHeaderTimeout 防 Slowloris 慢连接耗尽 fd。
	server := newHTTPServer(cfg.HTTPAddr, router)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runnerDone := make(chan struct{})
	runners := []backgroundRunner{maintenance, profileMaintenance, reminderRunner, assignmentReminderRunner, authReplayRunner}
	if telegramRunner != nil {
		runners = append(runners, telegramRunner)
	}
	go func() {
		// 所有后台任务共享同一 lifecycle，并在返回前全部响应 cancellation。
		var running sync.WaitGroup
		running.Add(len(runners))
		for _, runner := range runners {
			runner := runner
			go func() {
				defer running.Done()
				runner.Run(runCtx)
			}()
		}
		running.Wait()
		close(runnerDone)
	}()
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.ListenAndServe() }()
	lifecycle := serverLifecycle{
		timeout:      10 * time.Second,
		cancel:       cancel,
		shutdown:     server.Shutdown,
		serverResult: serverResult,
		runnerDone:   runnerDone,
	}
	if err := lifecycle.wait(ctx); err != nil {
		return newStartupFailure("http-serve", "HTTP_ADDR", "network", err)
	}
	return nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

type archiveCapabilityStartupReader interface {
	Current(context.Context) (planningcapability.ArchiveCapabilityState, error)
}

func composeShootPlanningApplication(
	ctx context.Context,
	startupReader archiveCapabilityStartupReader,
	executor *idempotency.Executor,
	projectors ...shootplanning.ShotAccessRefProjector,
) (*shootplanning.Application, error) {
	if startupReader == nil {
		return nil, errors.New("shoot planning archive capability startup reader is required")
	}
	archiveCapability, err := startupReader.Current(ctx)
	if err != nil {
		return nil, fmt.Errorf("read shoot planning archive capability at startup: %w", err)
	}
	options, err := shootPlanningOptionsForCapability(archiveCapability.Capability)
	if err != nil {
		return nil, err
	}
	if len(projectors) > 0 && projectors[0] != nil {
		options = append(options, shootplanning.WithShotAccessRefProjector(projectors[0]))
	}
	application, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), executor, options...)
	if err != nil {
		return nil, fmt.Errorf("compose shoot planning application: %w", err)
	}
	return application, nil
}

func composeShootPlanningApplicationWithIngestion(
	ctx context.Context,
	startupReader archiveCapabilityStartupReader,
	executor *idempotency.Executor,
	projector shootplanning.ShotAccessRefProjector,
	sink shootplanning.PlanReadyObservationSink,
) (*shootplanning.Application, error) {
	if startupReader == nil {
		return nil, errors.New("shoot planning archive capability startup reader is required")
	}
	archiveCapability, err := startupReader.Current(ctx)
	if err != nil {
		return nil, fmt.Errorf("read shoot planning archive capability at startup: %w", err)
	}
	options, err := shootPlanningOptionsForCapability(archiveCapability.Capability)
	if err != nil {
		return nil, err
	}
	options = append(options,
		shootplanning.WithIngestionRoutesEnabled(),
		shootplanning.WithPlanReadyObservationSink(sink),
	)
	if projector != nil {
		options = append(options, shootplanning.WithShotAccessRefProjector(projector))
	}
	application, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), executor, options...)
	if err != nil {
		return nil, fmt.Errorf("compose shoot planning application with ingestion: %w", err)
	}
	return application, nil
}

func shootPlanningOptionsForCapability(
	capability planningcapability.ArchiveCapability,
) ([]shootplanning.ApplicationOption, error) {
	// CRM reminder + settings timezone are decoupled from archive capability:
	// ITEM-6 production always wires the real CRM adapter (never Disabled/Noop).
	crmReminder := shootplanning.WithCRMReminder(reminder.NewCRMReminderLifecycleAdapter(nil), true)
	switch capability {
	case planningcapability.ArchiveCapabilityCore:
		return []shootplanning.ApplicationOption{crmReminder}, nil
	case planningcapability.ArchiveCapabilityPlanningShare:
		return []shootplanning.ApplicationOption{
			shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareArchiveImpactPolicyV1{}),
			shootplanning.WithReadinessRemovalGuard(planshare.ReadinessRemovalGuard{}),
			crmReminder,
		}, nil
	case planningcapability.ArchiveCapabilityReminder:
		return []shootplanning.ApplicationOption{
			shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareReminderArchiveImpactPolicyV1{}),
			shootplanning.WithReadinessRemovalGuard(planshare.ReadinessRemovalGuard{}),
			shootplanning.WithArchiveReminderParticipant(reminder.NewPlanArchiveReminderAdapter()),
			crmReminder,
		}, nil
	default:
		return nil, fmt.Errorf("shoot planning archive capability requires unavailable server wiring: %s", capability)
	}
}

type backgroundRunner interface {
	Run(context.Context)
}

func buildTelegramIntegration(
	cfg config.Config,
	accounts *store.Store,
	settingsSvc *settings.Service,
	reminderSvc *reminder.Service,
	freshness *reminder.AssignmentReminderFreshness,
	logger *slog.Logger,
) (httpapi.TelegramBindingIssuer, *digest.TelegramRunner, error) {
	enabled, err := cfg.TelegramStatus()
	if err != nil || !enabled {
		return nil, nil, err
	}
	if freshness == nil {
		return nil, nil, errors.New("assignment reminder freshness required for telegram digest intent")
	}

	gate := digest.NewRecipientGate()
	integrationState := digest.NewIntegrationState(logger)
	bindingRepo := digest.NewPostgresBindingRepository()
	tokenResolver := digest.NewBindTokenResolver(accounts, bindingRepo)
	binding := digest.NewBindingService(bindingRepo, tokenResolver, gate, cfg.TelegramBotUsername)
	targets := digest.NewSettingsTargetProvider(settingsSvc)
	scan := digest.NewReminderScanEnsurer(reminderSvc)
	chatResolver := digest.NewChatAccountResolver(accounts, bindingRepo)
	updateHandler := digest.NewUpdateHandler(binding, chatResolver, scan, targets, bindingRepo)
	telegramClient := telegramapi.NewClient(cfg.TelegramBotToken, telegramAPIBaseURL, nil)
	poller := digest.NewPoller(telegramClient, updateHandler).WithIntegrationState(integrationState)

	snapshots := digest.NewPostgresSnapshotRepository()
	messages := digest.NewDigestMessageBuilder(snapshots, digest.NewRenderer())
	recipients := digest.NewSettingsRecipientResolver(settingsSvc)
	deliveryRepo := digest.NewPostgresDeliveryRepository()
	intent := digest.NewDigestIntentService(messages, freshness)
	sender := digest.NewDeliverySender(deliveryRepo, gate, recipients, messages, telegramClient).
		WithIntegrationState(integrationState).
		WithIntentService(intent)
	senderRunner := digest.NewDeliverySenderRunner(accounts, sender).WithIntegrationState(integrationState)
	daily := digest.NewDailyScheduler(accounts, targets, scan, digest.NewPostgresDailyRepository(), logger)

	guardedBinding := digest.NewGuardedBindingIssuer(binding, integrationState)
	return guardedBinding, digest.NewTelegramRunner(poller, daily, senderRunner, logger), nil
}

type serverLifecycle struct {
	timeout      time.Duration
	cancel       context.CancelFunc
	shutdown     func(context.Context) error
	serverResult <-chan error
	runnerDone   <-chan struct{}
}

func (l serverLifecycle) wait(ctx context.Context) error {
	select {
	case err := <-l.serverResult:
		l.cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), l.timeout)
		defer shutdownCancel()
		if waitErr := waitForRunner(shutdownCtx, l.runnerDone); waitErr != nil {
			return waitErr
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		l.cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), l.timeout)
		defer shutdownCancel()
		shutdownErr := l.shutdown(shutdownCtx)
		if waitErr := waitForRunner(shutdownCtx, l.runnerDone); waitErr != nil {
			return waitErr
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return nil
	}
}

func waitForRunner(ctx context.Context, runnerDone <-chan struct{}) error {
	select {
	case <-runnerDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("background runner shutdown timeout: %w", ctx.Err())
	}
}

func derivePlanshareIPDigestKey(rootSecret string) []byte {
	extract := hmac.New(sha256.New, make([]byte, sha256.Size))
	_, _ = extract.Write([]byte(rootSecret))
	prk := extract.Sum(nil)
	expand := hmac.New(sha256.New, prk)
	_, _ = expand.Write([]byte("planshare/v1/ip-digest"))
	_, _ = expand.Write([]byte{1})
	return expand.Sum(nil)
}
