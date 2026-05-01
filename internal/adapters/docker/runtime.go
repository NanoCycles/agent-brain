package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type Runtime struct{}

func (Runtime) DockerAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run() == nil
}

func (Runtime) Neo4jRunning(ctx context.Context) bool {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", "127.0.0.1:7687")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (r Runtime) Up(ctx context.Context, composePath string) error {
	if !r.DockerAvailable(ctx) {
		return fmt.Errorf("docker is not available. Start Docker Desktop or your Docker daemon, then run agent-brain up again")
	}
	if err := writeComposeIfMissing(composePath); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composePath, "up", "-d")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up failed: %w: %s", err, string(out))
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if r.Neo4jRunning(ctx) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("neo4j did not become reachable on bolt://localhost:7687")
}

func (Runtime) Down(ctx context.Context, composePath string) error {
	if _, err := os.Stat(composePath); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composePath, "down")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose down failed: %w: %s", err, string(out))
	}
	return nil
}

func (r Runtime) Status(ctx context.Context) ports.RuntimeStatus {
	return ports.RuntimeStatus{DockerAvailable: r.DockerAvailable(ctx), Neo4jRunning: r.Neo4jRunning(ctx)}
}

func (Runtime) GraphStats(ctx context.Context) (domain.GraphStats, error) {
	return domain.GraphStats{}, nil
}

func writeComposeIfMissing(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(ComposeYAML), 0o644)
}

const ComposeYAML = `services:
  neo4j:
    image: neo4j:5-community
    container_name: agent-brain-neo4j
    ports:
      - "7474:7474"
      - "7687:7687"
    environment:
      NEO4J_AUTH: neo4j/agentbrain
    volumes:
      - agent-brain-neo4j-data:/data
volumes:
  agent-brain-neo4j-data:
`
