package app

import (
	"context"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type RuntimeService struct {
	runtime ports.RuntimeManager
	graph   ports.GraphStore
}

func NewRuntimeService(runtime ports.RuntimeManager, graph ports.GraphStore) *RuntimeService {
	return &RuntimeService{runtime: runtime, graph: graph}
}

func (s *RuntimeService) Up(ctx context.Context, spec ports.RuntimeSpec) error {
	return s.runtime.Up(ctx, spec)
}

func (s *RuntimeService) Down(ctx context.Context, spec ports.RuntimeSpec) error {
	return s.runtime.Down(ctx, spec)
}

type StatusReport struct {
	DockerAvailable bool
	Neo4jRunning    bool
	GraphStats      domain.GraphStats
	GraphStatsError string
}

func (s *RuntimeService) Status(ctx context.Context, spec ports.RuntimeSpec) StatusReport {
	st := s.runtime.Status(ctx, spec)
	report := StatusReport{DockerAvailable: st.DockerAvailable, Neo4jRunning: st.Neo4jRunning}
	if s.graph != nil && st.Neo4jRunning {
		stats, err := s.graph.Stats(ctx)
		if err != nil {
			report.GraphStatsError = err.Error()
		} else {
			report.GraphStats = stats
		}
	}
	return report
}

func RuntimeSpecFromConfig(cfg Config, composePath string) ports.RuntimeSpec {
	cfg = NormalizeConfig(cfg)
	return ports.RuntimeSpec{
		ComposePath:   composePath,
		Namespace:     cfg.RuntimeNamespace,
		ContainerName: "agent-brain-neo4j-" + cfg.RuntimeNamespace,
		VolumeName:    "agent-brain-neo4j-data-" + cfg.RuntimeNamespace,
		Neo4jHTTPPort: cfg.Neo4jHTTPPort,
		Neo4jBoltPort: cfg.Neo4jBoltPort,
		Neo4jUser:     cfg.Neo4jUser,
		Neo4jPassword: cfg.Neo4jPassword,
	}
}
