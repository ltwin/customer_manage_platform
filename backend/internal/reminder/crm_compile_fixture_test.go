package reminder_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

func TestCRMUnionCompilePositiveAndAdapters(t *testing.T) {
	t.Parallel()
	var facts []crm.CRMReminderLifecycleFactV1
	facts = append(facts,
		crm.CRMPlanLinkChangedFactV1{},
		crm.CRMOrderLifecycleChangedFactV1{},
		crm.CRMScheduleChangedFactV1{},
	)
	if len(facts) != 3 {
		t.Fatalf("expected 3 sealed CRM variants, got %d", len(facts))
	}
	var _ crm.CRMReminderLifecycleParticipant = (*reminder.CRMReminderLifecycleAdapter)(nil)
	var _ shootplanning.PlanArchiveReminderParticipant = (*reminder.PlanArchiveReminderAdapter)(nil)
	var _ reminder.TimezoneChangePlanningParticipant = reminder.NewTimezoneChangePlanningParticipant(nil, nil)
}

func TestCRMFourthVariantCompileNegative(t *testing.T) {
	backendRoot := backendModulePath(t)
	pkgDir := filepath.Join(backendRoot, "internal", "reminder", "compilefail_fourth_variant")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package compilefail_fourth_variant

import "github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"

type FakeFourth struct{}

var _ crm.CRMReminderLifecycleFactV1 = FakeFourth{}
`
	srcPath := filepath.Join(pkgDir, "fake.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(pkgDir) }()

	cmd := exec.Command("go", "test", "./internal/reminder/compilefail_fourth_variant/")
	cmd.Dir = backendRoot
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected fourth CRM variant to fail compile, output:\n%s", out)
	}
	combined := string(out)
	if !strings.Contains(combined, "CRMReminderLifecycleFactV1") &&
		!strings.Contains(combined, "does not implement") &&
		!strings.Contains(combined, "crmReminderLifecycleFactV1") {
		t.Fatalf("unexpected compile failure output:\n%s", combined)
	}
}

func TestCRMPackageMustNotImportReminderCompileNegative(t *testing.T) {
	backendRoot := backendModulePath(t)
	tmp := filepath.Join(backendRoot, "internal", "shootplanning", "crm", "zz_tmp_reminder_import_lint.go")
	content := "package crm\n\nimport _ \"github.com/samson/customer-manage-platform/backend/internal/reminder\"\n"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmp) }()

	// CRM→reminder is impossible without an import cycle (reminder already imports crm).
	build := exec.Command("go", "test", "./internal/shootplanning/crm/")
	build.Dir = backendRoot
	out, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("expected CRM→reminder import to fail; output:\n%s", out)
	}
	combined := string(out)
	if !strings.Contains(combined, "import cycle") && !strings.Contains(combined, "reminder") {
		t.Fatalf("expected import cycle or reminder denial, got:\n%s", combined)
	}
}

func backendModulePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
