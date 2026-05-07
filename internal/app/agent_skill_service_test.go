package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAgentSkillPacks(t *testing.T) {
	dir := t.TempDir()
	packs, err := GenerateAgentSkillPacks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) < 6 {
		t.Fatalf("expected all agent skill packs, got %d", len(packs))
	}
	codexPath := filepath.Join(dir, "codex", "agent-brain", "SKILL.md")
	data, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "change_start") || !strings.Contains(text, "cavernicola") {
		t.Fatalf("codex skill missing required workflow:\n%s", text)
	}
}

func TestInstallCursorRulesWritesProjectRule(t *testing.T) {
	root := t.TempDir()
	result, err := InstallCursorRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatalf("expected first install to change")
	}
	data, err := os.ReadFile(filepath.Join(root, ".cursor", "rules", "agent-brain.mdc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "plan_gate") {
		t.Fatalf("cursor rule missing plan_gate workflow")
	}
}
