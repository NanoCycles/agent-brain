package ports

import (
	"context"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

type MetadataStore interface {
	Init(ctx context.Context) error
	SaveRepository(ctx context.Context, repo domain.Repository) error
	SaveIndexRun(ctx context.Context, run domain.IndexRun) error
	SaveIndexedFiles(ctx context.Context, repoRoot string, files []domain.IndexedFile) error
	IndexedFiles(ctx context.Context, repoRoot string) ([]domain.IndexedFile, error)
	LastIndexRun(ctx context.Context, repoRoot string) (*domain.IndexRun, error)
	RelevantFiles(ctx context.Context, topics []string, limit int) ([]string, error)
	SaveContextPack(ctx context.Context, pack domain.ContextPack, mdPath, jsonPath string) error
	SaveMemoryProposal(ctx context.Context, taskID, path string) error
	SaveAppliedMemory(ctx context.Context, memory domain.MemoryRecord) error
	AppliedMemories(ctx context.Context, repoRoot string, limit int) ([]domain.MemoryRecord, error)
	SaveDomainMemory(ctx context.Context, repoRoot string, memory domain.DomainMemory) error
	DomainMemory(ctx context.Context, repoRoot string) (domain.DomainMemory, error)
	Close() error
}
