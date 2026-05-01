package ports

import (
	"context"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

type RuntimeStatus struct {
	DockerAvailable bool
	Neo4jRunning    bool
}

type RuntimeSpec struct {
	ComposePath   string
	Namespace     string
	ContainerName string
	VolumeName    string
	Neo4jHTTPPort int
	Neo4jBoltPort int
	Neo4jUser     string
	Neo4jPassword string
}

type RuntimeManager interface {
	DockerAvailable(ctx context.Context) bool
	Neo4jRunning(ctx context.Context, spec RuntimeSpec) bool
	Up(ctx context.Context, spec RuntimeSpec) error
	Down(ctx context.Context, spec RuntimeSpec) error
	Status(ctx context.Context, spec RuntimeSpec) RuntimeStatus
	GraphStats(ctx context.Context) (domain.GraphStats, error)
}
