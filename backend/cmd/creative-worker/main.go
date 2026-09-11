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

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
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
	media, err := creativemedia.Compose(cfg, deriveTicketKey(cfg.AuthTokenSecret))
	if err != nil {
		return err
	}
	// Media stages are the only registered handlers; generation stays absent.
	runtime, err := db.NewJobRuntime(media.Handlers(), slog.Default())
	if err != nil {
		return err
	}
	media.SetRuntime(runtime)
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	go sweepExpiredUploads(ctx, db, media)
	<-ctx.Done()
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
