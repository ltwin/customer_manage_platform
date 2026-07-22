package store

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestReadSnapshotUsesRepeatableReadAndReadOnlyTransaction(t *testing.T) {
	ctx := context.Background()
	_, scope := openReadSnapshotTestStore(t, "acct-export")

	var isolation, readOnly string
	err := scope.WithReadSnapshot(ctx, func(readScope ReadTxAccountScope) error {
		return readScope.scope.execRunner().QueryRow(ctx, `
			SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only')
		`).Scan(&isolation, &readOnly)
	})
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if isolation != "repeatable read" || readOnly != "on" {
		t.Fatalf("transaction properties = %q/%q, want repeatable read/on", isolation, readOnly)
	}

	readType := reflect.TypeOf(ReadTxAccountScope{})
	for _, forbidden := range []string{
		"Insert", "InsertReturningID", "InsertOnConflictDoNothingReturning", "Upsert",
		"Update", "Delete", "QueryRowForUpdate", "WithinTx", "WithTxScope",
	} {
		if _, exists := readType.MethodByName(forbidden); exists {
			t.Fatalf("ReadTxAccountScope must not expose %s", forbidden)
		}
	}
}

func TestReadSnapshotDoesNotObserveLaterCommittedWrites(t *testing.T) {
	ctx := context.Background()
	_, scope := openReadSnapshotTestStore(t, "acct-snapshot")
	if err := scope.Insert(ctx, "customers", []string{"id", "display_name", "channel"},
		"customer-before", "Before", "other"); err != nil {
		t.Fatalf("insert initial customer: %v", err)
	}

	var packagesDuringSnapshot int
	err := scope.WithReadSnapshot(ctx, func(readScope ReadTxAccountScope) error {
		customers, err := readScope.Query(ctx, "customers", "id", "")
		if err != nil {
			return err
		}
		for customers.Next() {
			var id string
			if err := customers.Scan(&id); err != nil {
				customers.Close()
				return err
			}
		}
		customers.Close()
		if err := customers.Err(); err != nil {
			return err
		}

		if err := scope.Insert(ctx, "packages",
			[]string{"id", "name", "shoot_type", "pricing_mode", "base_price"},
			"package-after", "After", "portrait", "fixed", 1000); err != nil {
			return err
		}
		packages, err := readScope.Query(ctx, "packages", "id", "")
		if err != nil {
			return err
		}
		defer packages.Close()
		for packages.Next() {
			packagesDuringSnapshot++
		}
		return packages.Err()
	})
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if packagesDuringSnapshot != 0 {
		t.Fatalf("snapshot observed %d package rows committed after its first read", packagesDuringSnapshot)
	}

	var packagesNextSnapshot int
	err = scope.WithReadSnapshot(ctx, func(readScope ReadTxAccountScope) error {
		rows, err := readScope.Query(ctx, "packages", "id", "")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			packagesNextSnapshot++
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("next read snapshot: %v", err)
	}
	if packagesNextSnapshot != 1 {
		t.Fatalf("next snapshot packages = %d, want 1", packagesNextSnapshot)
	}
}

func TestReadSnapshotReportsCommitFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, scope := openReadSnapshotTestStore(t, "acct-commit-failure")
	err := scope.WithReadSnapshot(ctx, func(readScope ReadTxAccountScope) error {
		var isolation string
		if err := readScope.scope.execRunner().QueryRow(ctx,
			`SELECT current_setting('transaction_isolation')`).Scan(&isolation); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "commit scoped read snapshot") {
		t.Fatalf("commit failure error = %v, want explicit commit stage", err)
	}
}

func openReadSnapshotTestStore(t *testing.T, accountID string) (*Store, AccountScope) {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	if err := MigrateUp(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.CreateAccount(ctx, accountID, "test-hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return database, database.ScopeFor(auth.AccountContext{AccountID: accountID})
}
