package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestSaveIndexedFilesRemovesStaleFiles(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	repo := "/repo"
	if err := store.SaveIndexedFiles(ctx, repo, []domain.IndexedFile{
		{Path: "a.go", Hash: "old", IndexedAt: time.Now()},
		{Path: "b.go", Hash: "same", IndexedAt: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIndexedFiles(ctx, repo, []domain.IndexedFile{
		{Path: "b.go", Hash: "same", IndexedAt: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	files, err := store.IndexedFiles(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "b.go" {
		t.Fatalf("expected stale file to be removed, got %#v", files)
	}
}
