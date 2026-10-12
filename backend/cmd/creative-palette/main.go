// Command creative-palette rebuild queues derived image palette reconstruction.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativepalette"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "rebuild" {
		return errors.New("usage: creative-palette rebuild --account <id> [--algorithm oklab-k5-v1]")
	}
	flags := flag.NewFlagSet("rebuild", flag.ContinueOnError)
	account := flags.String("account", "", "指定账号")
	algorithm := flags.String("algorithm", creativepalette.Algorithm, "算法版本")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *account == "" || flags.NArg() != 0 || *algorithm != creativepalette.Algorithm {
		return errors.New("必须指定账号与受支持的算法版本")
	}
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	// This operator process only inserts jobs; it never starts the runtime.
	runtime, err := db.NewJobRuntime([]store.JobHandler{{Kind: creativemedia.PaletteBackfillKind, Queue: "maintenance", Work: func(context.Context, store.AccountScope, jobs.Request) error { return jobs.ErrNoHandlers }}}, slog.Default())
	if err != nil {
		return err
	}
	if err = runtime.Check(ctx); err != nil {
		return err
	}
	if err = creativemedia.RebuildPalettes(ctx, db.ScopeFor(auth.AccountContext{AccountID: *account}), runtime, *algorithm); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "已排队：账号 %s，算法 %s\n", *account, *algorithm)
	return err
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
