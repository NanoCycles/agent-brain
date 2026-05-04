package app

import (
	"context"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type IndexService struct {
	indexer ports.CodeIndexer
	meta    ports.MetadataStore
	graph   ports.GraphStore
}

type IndexOptions struct {
	Incremental bool
}

type IndexResult struct {
	Index        domain.CodeIndex
	ChangedFiles []string
	Unchanged    bool
	GraphUpdated bool
}

func NewIndexService(indexer ports.CodeIndexer, meta ports.MetadataStore, graph ports.GraphStore) *IndexService {
	return &IndexService{indexer: indexer, meta: meta, graph: graph}
}

func (s *IndexService) Index(ctx context.Context, repoRoot string) (domain.CodeIndex, error) {
	result, err := s.IndexWithOptions(ctx, repoRoot, IndexOptions{})
	if err != nil {
		return domain.CodeIndex{}, err
	}
	return result.Index, nil
}

func (s *IndexService) IndexWithOptions(ctx context.Context, repoRoot string, opts IndexOptions) (IndexResult, error) {
	start := time.Now().UTC()
	idx, err := s.indexer.Index(ctx, repoRoot)
	if err != nil {
		return IndexResult{}, err
	}
	if err := s.meta.Init(ctx); err != nil {
		return IndexResult{}, err
	}
	previous, _ := s.meta.IndexedFiles(ctx, idx.Repository.Root)
	changed := changedIndexedFiles(previous, idx.Files)
	if err := s.meta.SaveRepository(ctx, idx.Repository); err != nil {
		return IndexResult{}, err
	}
	if err := s.meta.SaveIndexedFiles(ctx, idx.Repository.Root, idx.Files); err != nil {
		return IndexResult{}, err
	}
	graphUpdated := false
	if s.graph != nil {
		if err := s.graph.Ping(ctx); err == nil && (!opts.Incremental || len(changed) > 0 || len(previous) == 0) {
			if opts.Incremental && len(previous) > 0 {
				if err := s.graph.SaveIndexChanges(ctx, idx, changed); err != nil {
					return IndexResult{}, err
				}
			} else {
				if err := s.graph.SaveIndex(ctx, idx); err != nil {
					return IndexResult{}, err
				}
			}
			graphUpdated = true
		}
	}
	run := domain.IndexRun{RepoRoot: idx.Repository.Root, CommitSHA: idx.Repository.CommitSHA, StartedAt: start, CompletedAt: time.Now().UTC(), FileCount: len(idx.Files), NodeCount: len(idx.Nodes), RelCount: len(idx.Relations)}
	if err := s.meta.SaveIndexRun(ctx, run); err != nil {
		return IndexResult{}, err
	}
	return IndexResult{Index: idx, ChangedFiles: changed, Unchanged: opts.Incremental && len(changed) == 0 && len(previous) > 0, GraphUpdated: graphUpdated}, nil
}

func changedIndexedFiles(previous, current []domain.IndexedFile) []string {
	prev := map[string]string{}
	for _, file := range previous {
		prev[file.Path] = file.Hash
	}
	var changed []string
	seen := map[string]struct{}{}
	for _, file := range current {
		seen[file.Path] = struct{}{}
		if prev[file.Path] != file.Hash {
			changed = append(changed, file.Path)
		}
	}
	for _, file := range previous {
		if _, ok := seen[file.Path]; !ok {
			changed = append(changed, file.Path)
		}
	}
	return changed
}
