package ports

import (
	"context"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

type RuntimeStatus struct {
	DockerAvailable bool
	Neo4jRunning    bool
}

type RuntimeManager interface {
	DockerAvailable(ctx context.Context) bool
	Neo4jRunning(ctx context.Context) bool
	Up(ctx context.Context, composePath string) error
	Down(ctx context.Context, composePath string) error
	Status(ctx context.Context) RuntimeStatus
	GraphStats(ctx context.Context) (domain.GraphStats, error)
}
