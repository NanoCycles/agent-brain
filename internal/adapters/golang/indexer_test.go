package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexerIndexesGoFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "domain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package domain
type User struct { ID string }
type Repo interface { Save(User) error }
func NewUser() User { return User{} }
func (User) Name() string { return "" }
`
	if err := os.WriteFile(filepath.Join(dir, "user.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Files) != 1 {
		t.Fatalf("expected 1 indexed file, got %d", len(idx.Files))
	}
	f := idx.Files[0]
	if f.Layer != "domain" || len(f.Structs) != 1 || len(f.Interfaces) != 1 || len(f.Functions) != 1 || len(f.Methods) != 1 {
		t.Fatalf("unexpected parsed file: %#v", f)
	}
}
