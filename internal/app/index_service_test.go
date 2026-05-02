package app

import (
	"context"
	"testing"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestIndexIncrementalSkipsGraphWhenHashesUnchanged(t *testing.T) {
	ctx := context.Background()
	idx := domain.CodeIndex{
		Repository: domain.Repository{Root: "/repo", Name: "repo", IndexedAt: time.Now()},
		Files:      []domain.IndexedFile{{Path: "main.go", Hash: "abc"}},
	}
	meta := &fakeMetaStore{files: idx.Files}
	graph := &fakeGraphStore{}

	result, err := NewIndexService(fakeIndexer{idx: idx}, meta, graph).IndexWithOptions(ctx, "/repo", IndexOptions{Incremental: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Unchanged {
		t.Fatal("expected unchanged incremental result")
	}
	if graph.saved {
		t.Fatal("expected graph write to be skipped")
	}
}

type fakeIndexer struct {
	idx domain.CodeIndex
}

func (f fakeIndexer) Index(context.Context, string) (domain.CodeIndex, error) { return f.idx, nil }

type fakeGraphStore struct {
	saved bool
}

func (f *fakeGraphStore) Ping(context.Context) error { return nil }
func (f *fakeGraphStore) SaveIndex(context.Context, domain.CodeIndex) error {
	f.saved = true
	return nil
}
func (f *fakeGraphStore) Stats(context.Context) (domain.GraphStats, error) {
	return domain.GraphStats{}, nil
}
func (f *fakeGraphStore) SearchImpact(context.Context, string, int) ([]domain.GraphNode, error) {
	return nil, nil
}
func (f *fakeGraphStore) ExpandImpact(context.Context, string, []string, int) ([]domain.GraphNode, error) {
	return nil, nil
}
func (f *fakeGraphStore) SaveMemory(context.Context, domain.MemoryRecord) error { return nil }
func (f *fakeGraphStore) SaveDomainMemory(context.Context, string, domain.DomainMemory) error {
	return nil
}
func (f *fakeGraphStore) Close(context.Context) error { return nil }

type fakeMetaStore struct {
	files []domain.IndexedFile
}

func (f *fakeMetaStore) Init(context.Context) error                              { return nil }
func (f *fakeMetaStore) SaveRepository(context.Context, domain.Repository) error { return nil }
func (f *fakeMetaStore) SaveIndexRun(context.Context, domain.IndexRun) error     { return nil }
func (f *fakeMetaStore) SaveIndexedFiles(_ context.Context, _ string, files []domain.IndexedFile) error {
	f.files = files
	return nil
}
func (f *fakeMetaStore) IndexedFiles(context.Context, string) ([]domain.IndexedFile, error) {
	return f.files, nil
}
func (f *fakeMetaStore) LastIndexRun(context.Context, string) (*domain.IndexRun, error) {
	return nil, nil
}
func (f *fakeMetaStore) RelevantFiles(context.Context, []string, int) ([]string, error) {
	return nil, nil
}
func (f *fakeMetaStore) SaveContextPack(context.Context, domain.ContextPack, string, string) error {
	return nil
}
func (f *fakeMetaStore) SaveMemoryProposal(context.Context, string, string) error { return nil }
func (f *fakeMetaStore) SaveAppliedMemory(context.Context, domain.MemoryRecord) error {
	return nil
}
func (f *fakeMetaStore) AppliedMemories(context.Context, string, int) ([]domain.MemoryRecord, error) {
	return nil, nil
}
func (f *fakeMetaStore) SaveDomainMemory(context.Context, string, domain.DomainMemory) error {
	return nil
}
func (f *fakeMetaStore) DomainMemory(context.Context, string) (domain.DomainMemory, error) {
	return domain.DomainMemory{}, nil
}
func (f *fakeMetaStore) Close() error { return nil }
