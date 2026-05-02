package ports

import (
	"context"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

type GraphStore interface {
	Ping(ctx context.Context) error
	SaveIndex(ctx context.Context, index domain.CodeIndex) error
	Stats(ctx context.Context) (domain.GraphStats, error)
	SearchImpact(ctx context.Context, topic string, limit int) ([]domain.GraphNode, error)
	ExpandImpact(ctx context.Context, repoRoot string, topics []string, limit int) ([]domain.GraphNode, error)
	SaveMemory(ctx context.Context, memory domain.MemoryRecord) error
	Close(ctx context.Context) error
}
