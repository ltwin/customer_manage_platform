package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const currentAuthSchemaVersion = 13

var (
	buildRevisionPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	configFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type authReadinessState struct {
	SchemaVersion      int
	DatabaseReady      bool
	LimiterSchemaReady bool
	LegacyCutoverReady bool
}

type authReadinessProbe interface {
	Inspect(context.Context) (authReadinessState, error)
}

type readinessProbeFactory func(context.Context) (authReadinessProbe, func(), error)

type authReadinessReport struct {
	Version           int               `json:"version"`
	GeneratedAt       string            `json:"generated_at"`
	BuildRevision     string            `json:"build_revision"`
	SchemaVersion     string            `json:"schema_version"`
	ConfigFingerprint string            `json:"config_fingerprint"`
	Environment       string            `json:"environment"`
	Status            string            `json:"status"`
	EvidencePath      string            `json:"evidence_path"`
	Checks            map[string]string `json:"checks"`
}

func runAuthReadiness(ctx context.Context, args []string, deps commandDeps) int {
	flags := flag.NewFlagSet("accountctl-auth-readiness", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	buildRevision := flags.String("build-revision", "", "")
	configFingerprint := flags.String("config-fingerprint", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		!buildRevisionPattern.MatchString(*buildRevision) ||
		!configFingerprintPattern.MatchString(*configFingerprint) ||
		*buildRevision != deps.buildRevision {
		return writeFailure(deps.stderr, "readiness", exitInvalidInput, "invalid_input", "提供当前 build revision 与 config fingerprint")
	}

	probe, closeProbe, err := deps.newReadinessProbe(ctx)
	if err != nil {
		return writeFailure(deps.stderr, "readiness", exitInternal, "internal", "检查数据库连接")
	}
	defer closeProbe()
	state, err := probe.Inspect(ctx)
	if err != nil {
		return writeFailure(deps.stderr, "readiness", exitInternal, "internal", "检查数据库 readiness")
	}
	checks := map[string]string{
		"database":       readinessResult(state.DatabaseReady),
		"limiter_schema": readinessResult(state.LimiterSchemaReady && state.SchemaVersion == currentAuthSchemaVersion),
		"legacy_cutover": readinessResult(state.LegacyCutoverReady),
	}
	status := "passed"
	exitCode := exitOK
	for _, result := range checks {
		if result != "passed" {
			status = "failed"
			exitCode = exitNotReady
		}
	}
	report := authReadinessReport{
		Version:           1,
		GeneratedAt:       deps.now().UTC().Format(time.RFC3339),
		BuildRevision:     *buildRevision,
		SchemaVersion:     strconv.Itoa(state.SchemaVersion),
		ConfigFingerprint: *configFingerprint,
		Environment:       "production",
		Status:            status,
		EvidencePath:      "accountctl://auth/readiness",
		Checks:            checks,
	}
	if err := json.NewEncoder(deps.stdout).Encode(report); err != nil {
		return exitInternal
	}
	return exitCode
}

func readinessResult(ready bool) string {
	if ready {
		return "passed"
	}
	return "failed"
}

type storeReadinessProbe struct {
	database *store.Store
}

func (probe storeReadinessProbe) Inspect(ctx context.Context) (authReadinessState, error) {
	state, err := probe.database.InspectAuthReadiness(ctx)
	if err != nil {
		return authReadinessState{}, err
	}
	return authReadinessState{
		SchemaVersion:      state.SchemaVersion,
		DatabaseReady:      state.DatabaseReady,
		LimiterSchemaReady: state.LimiterSchemaReady,
		LegacyCutoverReady: state.LegacyCutoverReady,
	}, nil
}

func newStoreReadinessProbe(ctx context.Context) (authReadinessProbe, func(), error) {
	database, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, nil, err
	}
	return storeReadinessProbe{database: database}, database.Close, nil
}
