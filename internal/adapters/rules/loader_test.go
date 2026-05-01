package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderLoad(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "testing.rules.yml"), []byte("name: testing\nrules:\n  - id: testing.unit\n    title: Unit tests required\n    severity: medium\n    topics: [testing]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := Loader{}.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || len(sets[0].Rules) != 1 {
		t.Fatalf("unexpected rules: %#v", sets)
	}
}
