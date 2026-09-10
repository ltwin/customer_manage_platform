package store

import (
	"io"
	"log/slog"
	"testing"

	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestCreativeQueueReadinessRequiresAllMigrations(t *testing.T) {
	s, err := Open(t.Context(), storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := s.NewJobRuntime(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err == nil {
		t.Fatal("unmigrated queue reported ready")
	}
	if _, err := s.pool.Exec(t.Context(), "CREATE SCHEMA creative_jobs"); err != nil {
		t.Fatal(err)
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(s.pool), &rivermigrate.Config{Schema: creativeQueueSchema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(t.Context(), rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{TargetVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err == nil {
		t.Error("partial migration reported ready")
	}
	if err := s.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err != nil {
		t.Fatalf("complete migration not ready: %v", err)
	}
}
