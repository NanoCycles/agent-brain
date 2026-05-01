package ports

import (
	"context"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

type CodeIndexer interface {
	Index(ctx context.Context, repoRoot string) (domain.CodeIndex, error)
}
