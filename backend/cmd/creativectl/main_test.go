package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// recorder stands in for the skill service. The protocol itself is covered by
// the domain's own tests; what needs proving here is that a command refuses
// what it should before it ever reaches a database or an object store.
type recorder struct {
	intent creativeskill.PublishIntent
	calls  int
}

func (r *recorder) Publish(_ context.Context, _ store.AccountScope, _ creativeskill.Package, intent creativeskill.PublishIntent) (creativeskill.Version, error) {
	r.calls++
	r.intent = intent
	return creativeskill.Version{
		ID: "ccsv_1", SkillID: "ccsk_1", VersionNumber: 1, Digest: "d", SkillRevision: 2,
	}, nil
}

func (r *recorder) ReclaimExpiredImports(context.Context, store.AccountScope, int) (creativeskill.Reclaimed, error) {
	r.calls++
	return creativeskill.Reclaimed{Imports: 2, Objects: 3}, nil
}

func harness(t *testing.T, cfg config.Config) (*recorder, *bytes.Buffer, commandDeps) {
	t.Helper()
	app := &recorder{}
	out := &bytes.Buffer{}
	return app, out, commandDeps{
		stdout: out, stderr: out,
		loadConfig: func() (config.Config, error) { return cfg, nil },
		open: func(context.Context, config.Config, string) (application, store.AccountScope, func(), error) {
			return app, store.AccountScope{}, func() {}, nil
		},
	}
}

// A package directory an operator can read is the whole input format.
func packageDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"manifest_schema_version":1,"key":"reference-direction","version":1,` +
		`"display_name":"名","description":"述","input_kinds":["text"],"max_inputs":1,` +
		`"tool_allowlist":["read_nodes@1"],"required_model_capabilities":[],` +
		`"output_contract":"契约","completion_check_key":"k"}`
	write := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", manifest)
	write("SKILL.md", "正文")
	write("references/checklist.md", "清单")
	return dir
}

const operationID = "4d8f4d0e-7a2b-4c6d-9f31-5eed00000001"

func TestImportPublishesTheDirectoryItWasPointedAt(t *testing.T) {
	app, out, deps := harness(t, config.Config{})
	code := run(t.Context(), []string{"skill", "import",
		"--package-dir", packageDir(t), "--account", "acct-1",
		"--operation-id", operationID, "--activate"}, deps)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}
	if app.intent.Origin != "account" || !app.intent.Activate || app.intent.OperationID != operationID {
		t.Fatalf("published with %+v", app.intent)
	}
	var report importReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "published" || report.VersionID != "ccsv_1" {
		t.Fatalf("report %+v", report)
	}
	// The next import of this skill has to pass this number as
	// --expected-revision, so a receipt without it forces a database query.
	if report.SkillRevision != 2 {
		t.Fatalf("the receipt omits the revision the next import needs: %+v", report)
	}
}

// Which account owns the platform catalog is a deployment fact. A flag that
// could mint a platform skill by naming any account would make every photographer's
// catalog reachable by a typo.
func TestPlatformOriginIsBoundToTheConfiguredPublisher(t *testing.T) {
	dir := packageDir(t)
	for _, tc := range []struct {
		name    string
		cfg     config.Config
		account string
		want    int
	}{
		{"配置的发布账号", config.Config{CreativePlatformAccountID: "platform-1"}, "platform-1", exitOK},
		{"别的账号", config.Config{CreativePlatformAccountID: "platform-1"}, "acct-1", exitInvalidInput},
		{"未配置发布账号", config.Config{}, "platform-1", exitNotReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, out, deps := harness(t, tc.cfg)
			code := run(t.Context(), []string{"skill", "import",
				"--package-dir", dir, "--account", tc.account,
				"--operation-id", operationID, "--origin", "platform"}, deps)
			if code != tc.want {
				t.Fatalf("exit %d, want %d: %s", code, tc.want, out)
			}
			if tc.want != exitOK && app.calls != 0 {
				t.Fatal("a refused import still reached the service")
			}
		})
	}
}

// Every rejection happens before the service is touched, so a bad invocation
// cannot leave a half-made import behind.
func TestBadInvocationsAreRefusedBeforeAnythingIsPublished(t *testing.T) {
	dir := packageDir(t)
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"没有子命令", []string{"skill"}, exitInvalidInput},
		{"不认识的子命令", []string{"skill", "publish"}, exitInvalidInput},
		{"operation id 不是 UUID", []string{"skill", "import", "--package-dir", dir,
			"--account", "a", "--operation-id", "seed-1"}, exitInvalidInput},
		{"缺少 account", []string{"skill", "import", "--package-dir", dir,
			"--operation-id", operationID}, exitInvalidInput},
		{"origin 取值非法", []string{"skill", "import", "--package-dir", dir, "--account", "a",
			"--operation-id", operationID, "--origin", "market"}, exitInvalidInput},
		{"包目录不存在", []string{"skill", "import", "--package-dir", filepath.Join(dir, "missing"),
			"--account", "a", "--operation-id", operationID}, exitInvalidInput},
		{"reclaim 缺少 account", []string{"skill", "reclaim"}, exitInvalidInput},
		{"reclaim limit 非正", []string{"skill", "reclaim", "--account", "a", "--limit", "0"}, exitInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, out, deps := harness(t, config.Config{})
			if code := run(t.Context(), tc.args, deps); code != tc.want {
				t.Fatalf("exit %d, want %d: %s", code, tc.want, out)
			}
			if app.calls != 0 {
				t.Fatal("a refused invocation still reached the service")
			}
		})
	}
}

func TestReclaimReportsWhatOneSweepDid(t *testing.T) {
	_, out, deps := harness(t, config.Config{})
	if code := run(t.Context(), []string{"skill", "reclaim", "--account", "acct-1"}, deps); code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}
	var report reclaimReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	// "Nothing was due" and "nothing could be deleted" must not look the same
	// to whoever reads this output.
	if report.Imports != 2 || report.Objects != 3 || report.Status != "swept" {
		t.Fatalf("report %+v", report)
	}
}
