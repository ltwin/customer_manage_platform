package creativeagent

import (
	"strings"
	"testing"
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
