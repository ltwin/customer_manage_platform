// Command server 是 CRM 单体后端入口。
// 启动序（design 2.2）：加载 env 配置 → migrate up → ensure 默认账号 → HTTP 监听。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarimage"
	"github.com/samson/customer-manage-platform/backend/internal/customer/avatarstore"
	"github.com/samson/customer-manage-platform/backend/internal/dashboard"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("启动失败", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := store.MigrateUp(cfg.DatabaseURL); err != nil {
		return err
	}

	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer s.Close()

	created, err := auth.EnsureDefaultAccount(ctx, s, cfg.SeedAdminPassword)
	if err != nil {
		return err
	}
	if created {
		logger.Info("已创建默认账号（seed 完成后可从环境移除 SEED_ADMIN_PASSWORD）")
	}
	objects, err := avatarstore.NewLocal(cfg.AvatarLocalRoot)
	if err != nil {
		return err
	}
	avatarRepo := customer.NewPostgresAvatarRepository()
	avatarApp := customer.NewAvatarApplication(avatarRepo, objects)
	maintenance := customer.NewAvatarMaintenanceRunner(s, avatarRepo, objects, logger)

	settingsSvc := settings.NewService(settings.NewPostgresRepository()).WithScopeFactory(func(accountID string) store.AccountScope {
		return s.ScopeFor(auth.AccountContext{AccountID: accountID})
	})
	reminderSvc := reminder.NewService(
		reminder.NewPostgresRepository(),
		reminder.NewSettingsAdapter(settingsSvc),
		logger,
	)
	reminderRunner := reminder.NewScanRunner(s, reminderSvc, settingsSvc, logger)
	dashboardSvc := dashboard.NewService(dashboard.NewPostgresRepository(), settingsSvc)

	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:          logger,
		DB:              s,
		ScopeFactory:    s,
		Auth:            auth.NewService(s, auth.NewTokenIssuer(cfg.AuthTokenSecret)),
		Customer:        customer.NewService(customer.NewPostgresRepository()),
		Orders:          order.NewService(order.NewPostgresRepository()),
		Packages:        pkgcatalog.NewService(pkgcatalog.NewPostgresRepository()),
		Idempotency:     idempotency.NewExecutor(),
		AccountTimezone: settingsSvc,
		Schedule:        schedule.NewService(schedule.NewPostgresRepository(), schedule.ClockFunc(time.Now)),
		Avatar:          avatarApp,
		AvatarProcessor: avatarimage.NewProcessor(),
		Settings:        settingsSvc,
		Reminders:       reminderSvc,
		Dashboard:       dashboardSvc,
	})

	logger.Info("HTTP 监听", slog.String("addr", cfg.HTTPAddr))
	// 公网直挂无反代（design D6），ReadHeaderTimeout 防 Slowloris 慢连接耗尽 fd。
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runnerDone := make(chan struct{})
	go func() {
		// 两个 runner 共享同一 lifecycle；任一退出都算 runner 结束（有界退出）。
		done := make(chan struct{}, 2)
		go func() { maintenance.Run(runCtx); done <- struct{}{} }()
		go func() { reminderRunner.Run(runCtx); done <- struct{}{} }()
		<-done
		<-done
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
	return lifecycle.wait(ctx)
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
