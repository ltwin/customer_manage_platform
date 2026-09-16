// Command creative-worker owns the new queue process, separate from the API.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func run() error {
	migrate := flag.Bool("migrate", false, "apply River-owned migrations in the creative_jobs schema")
	check := flag.Bool("check", false, "check queue schema without starting workers")
	flag.Parse()
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	db, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if *migrate {
		return db.MigrateCreativeJobs(ctx)
	}
	if *check {
		runtime, err := db.NewJobRuntime(nil, slog.Default())
		if err != nil {
			return err
		}
		return runtime.Check(ctx)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	media, err := creativemedia.Compose(cfg, deriveTicketKey(cfg.AuthTokenSecret), slog.Default())
	if err != nil {
		return err
	}
	// The worker is the process that actually executes a run, so it needs the
	// same gateway and skill directory the API composed — not a second, quietly
	// different one. Both read the deployment's own configuration.
	gatewayOptions, err := llmgateway.OptionsFromEnv()
	if err != nil {
		return err
	}
	if gatewayOptions.Catalog, err = llmgateway.OpenCatalog(gatewayOptions.CatalogPath, gatewayOptions.Credential); err != nil {
		return err
	}
	gatewayOptions.Logger = slog.Default()
	gateway, err := llmgateway.Build(gatewayOptions)
	if err != nil {
		return err
	}
	skills, err := creativeskill.Compose(cfg, db)
	if err != nil {
		return err
	}
	agent, err := creativeagent.NewService(gateway, gatewayOptions.Catalog, skills)
	if err != nil {
		return err
	}
	agent.SetLogger(slog.Default())
	// Media stages and agent runs are the registered handlers; generation stays
	// absent until FND-13.
	runtime, err := db.NewJobRuntime(append(media.Handlers(), agent.Handlers()...), slog.Default())
	if err != nil {
		return err
	}
	media.SetRuntime(runtime)
	agent.SetRuntime(runtime)
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	go sweepExpiredUploads(ctx, db, media)
	// Recovery needs no provider credentials or catalog: it only reconciles
	// persisted dispatches through account-scoped transactions.
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		sweepLLMDispatches(ctx, db)
	}()
	<-ctx.Done()
	<-recoveryDone
	stopCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	return runtime.Stop(stopCtx)
}

// deriveTicketKey must match cmd/server so tickets issued by the API and
// part signatures produced here validate in both processes.
func deriveTicketKey(rootSecret string) []byte {
	extract := hmac.New(sha256.New, make([]byte, sha256.Size))
	_, _ = extract.Write([]byte(rootSecret))
	prk := extract.Sum(nil)
	expand := hmac.New(sha256.New, prk)
	_, _ = expand.Write([]byte("creative-media/v1/ticket"))
	_, _ = expand.Write([]byte{1})
	return expand.Sum(nil)
}

// sweepExpiredUploads marks overdue sessions per active account. Objects
// are never deleted here; retention/GC arrives with its own feature.
func sweepExpiredUploads(ctx context.Context, db *store.Store, media *creativemedia.Service) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := media.SweepAllAccounts(ctx, db, 200); err != nil {
				slog.Warn("creative media sweep", slog.String("error", err.Error()))
			}
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "creative worker: %v\n", err)
		os.Exit(1)
	}
}

// sweepLLMDispatches runs immediately on restart, then drains account pages.
func sweepLLMDispatches(ctx context.Context, db *store.Store) {
	gateway := llmgateway.NewRecoveryService()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	cursor := ""
	for {
		sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		next, err := gateway.SweepExpiredDispatches(sweepCtx, db, cursor, 100)
		cancel()
		cursor = next
		if err != nil && ctx.Err() == nil {
			slog.Warn("llm dispatch recovery", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
