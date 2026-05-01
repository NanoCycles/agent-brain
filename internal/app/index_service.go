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

func NewIndexService(indexer ports.CodeIndexer, meta ports.MetadataStore, graph ports.GraphStore) *IndexService {
	return &IndexService{indexer: indexer, meta: meta, graph: graph}
}

func (s *IndexService) Index(ctx context.Context, repoRoot string) (domain.CodeIndex, error) {
	start := time.Now().UTC()
	idx, err := s.indexer.Index(ctx, repoRoot)
	if err != nil {
		return domain.CodeIndex{}, err
	}
	if err := s.meta.Init(ctx); err != nil {
		return domain.CodeIndex{}, err
	}
	if err := s.meta.SaveRepository(ctx, idx.Repository); err != nil {
		return domain.CodeIndex{}, err
	}
	if err := s.meta.SaveIndexedFiles(ctx, idx.Repository.Root, idx.Files); err != nil {
		return domain.CodeIndex{}, err
	}
	if s.graph != nil {
		if err := s.graph.Ping(ctx); err == nil {
			if err := s.graph.SaveIndex(ctx, idx); err != nil {
				return domain.CodeIndex{}, err
			}
		}
	}
	run := domain.IndexRun{RepoRoot: idx.Repository.Root, CommitSHA: idx.Repository.CommitSHA, StartedAt: start, CompletedAt: time.Now().UTC(), FileCount: len(idx.Files), NodeCount: len(idx.Nodes), RelCount: len(idx.Relations)}
	if err := s.meta.SaveIndexRun(ctx, run); err != nil {
		return domain.CodeIndex{}, err
	}
	return idx, nil
}
