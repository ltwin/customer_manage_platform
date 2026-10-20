package creativeagent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
)

func TestBaseInstructionCoversRegisteredToolsAndPrecedesSkill(t *testing.T) {
	for _, entry := range runtimeToolEntries() {
		if entry.Key == loadSkillTool || entry.Key == readSkillTool || entry.Key == readResultTool {
			continue
		}
		if !strings.Contains(assistantInstruction, entry.Key) {
			t.Errorf("missing %s", entry.Key)
		}
	}
	for _, name := range []string{setNodePromptTool, generateNodeTool, compressImageTool} {
		if !strings.Contains(assistantInstruction, name) {
			t.Errorf("missing %s", name)
		}
	}
	joined := joinInstruction("skill instruction")
	if !strings.HasPrefix(joined, strings.TrimSpace(assistantInstruction)) || !strings.HasSuffix(joined, "skill instruction") {
		t.Fatal("skill changed instruction order")
	}
}

// Model-visible words name the person neutrally: the creator may be a
// photographer today and anything else tomorrow (owner 2026-10-14). Every
// string literal of the package is checked, which covers tool descriptions,
// refusals and instructions alike; comments are free to say photographer.
func TestModelVisibleTextNamesNoProfession(t *testing.T) {
	if strings.Contains(assistantInstruction, "摄影师") {
		t.Error("base instruction names a profession")
	}
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "摄影师") {
				t.Errorf("%s names a profession", fset.Position(lit.Pos()))
			}
			return true
		})
	}
}

// AC-15: the repaint guidance names the fields read_canvas gives an export
// image, taken from the types themselves, so renaming one fails here instead
// of leaving the guidance pointing at nothing.
func TestRepaintGuidanceNamesTheExportSource(t *testing.T) {
	tag := func(v any, field string) string {
		f, ok := reflect.TypeOf(v).FieldByName(field)
		if !ok {
			t.Fatalf("no field %s", field)
		}
		return strings.Split(f.Tag.Get("json"), ",")[0]
	}
	names := []string{
		tag(canvasNodeContext{}, "ExportSource"), tag(exportSourceContext{}, "Lens"), tag(exportSourceContext{}, "State"),
		tag(creativecanvas.SceneExportLens{}, "DepthOfField"), tag(creativecanvas.SceneExportLens{}, "FocusObjectName"), tag(creativecanvas.SceneExportLens{}, "FocusM"),
		creativecanvas.BasedOnStale,
	}
	for _, name := range names {
		if !strings.Contains(assistantInstruction, name) {
			t.Errorf("the repaint guidance does not name %s", name)
		}
	}
}
