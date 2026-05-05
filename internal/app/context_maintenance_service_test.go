package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/platform/paths"
)

func TestCleanGeneratedContextRequiresConfirmation(t *testing.T) {
	_, err := CleanGeneratedContext(paths.ProjectPaths{}, false)
	if err == nil {
		t.Fatal("expected confirmation error")
	}
}

func TestCleanGeneratedContextRemovesOnlyAgentPacks(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.AIContextDir, p.ContextDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removeMe := []string{
		filepath.Join(p.AIContextDir, "AK-1.agent.md"),
		filepath.Join(p.AIContextDir, "AK-1.agent.json"),
		filepath.Join(p.ContextDir, "AK-2.agent.md"),
	}
	keepMe := filepath.Join(p.AIContextDir, "notes.md")
	for _, path := range append(removeMe, keepMe) {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := CleanGeneratedContext(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != len(removeMe) {
		t.Fatalf("expected %d removed files, got %#v", len(removeMe), result.Removed)
	}
	for _, path := range removeMe {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed", path)
		}
	}
	if _, err := os.Stat(keepMe); err != nil {
		t.Fatalf("expected non-agent context file to remain: %v", err)
	}
}
