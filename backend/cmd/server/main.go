// Command server 是 CRM 单体后端入口。
// 启动序（design 2.2）：加载 env 配置 → migrate up → ensure 默认账号 → HTTP 监听。
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(context.Background(), logger); err != nil {
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

	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger: logger,
		DB:     s,
		Auth:   auth.NewService(s, auth.NewTokenIssuer(cfg.AuthTokenSecret)),
	})

	logger.Info("HTTP 监听", slog.String("addr", cfg.HTTPAddr))
	return http.ListenAndServe(cfg.HTTPAddr, router)
}
