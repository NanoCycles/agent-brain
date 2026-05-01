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

func (s *RuntimeService) Up(ctx context.Context, composePath string) error {
	return s.runtime.Up(ctx, composePath)
}

func (s *RuntimeService) Down(ctx context.Context, composePath string) error {
	return s.runtime.Down(ctx, composePath)
}

type StatusReport struct {
	DockerAvailable bool
	Neo4jRunning    bool
	GraphStats      domain.GraphStats
	GraphStatsError string
}

func (s *RuntimeService) Status(ctx context.Context) StatusReport {
	st := s.runtime.Status(ctx)
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
