// Command creative-worker owns the new queue process, separate from the API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	// FND-01 intentionally registers no pretend content/generation handlers.
	runtime, err := db.NewJobRuntime(nil, slog.Default())
	if err != nil {
		return err
	}
	if *check {
		return runtime.Check(ctx)
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	stopCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	return runtime.Stop(stopCtx)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "creative worker: %v\n", err)
		os.Exit(1)
	}
}
