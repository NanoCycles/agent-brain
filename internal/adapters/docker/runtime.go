package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type Runtime struct{}

func (Runtime) DockerAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run() == nil
}

func (Runtime) Neo4jRunning(ctx context.Context, spec ports.RuntimeSpec) bool {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", spec.Neo4jBoltPort))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (r Runtime) Up(ctx context.Context, spec ports.RuntimeSpec) error {
	if !r.DockerAvailable(ctx) {
		return fmt.Errorf("docker is not available. Start Docker Desktop or your Docker daemon, then run agent-brain up again")
	}
	if err := writeCompose(spec); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", spec.Namespace, "-f", spec.ComposePath, "up", "-d")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up failed: %w: %s", err, string(out))
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if r.Neo4jRunning(ctx, spec) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("neo4j did not become reachable on bolt://localhost:%d", spec.Neo4jBoltPort)
}

func (Runtime) Down(ctx context.Context, spec ports.RuntimeSpec) error {
	if _, err := os.Stat(spec.ComposePath); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", spec.Namespace, "-f", spec.ComposePath, "down")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose down failed: %w: %s", err, string(out))
	}
	return nil
}

func (Runtime) Destroy(ctx context.Context, spec ports.RuntimeSpec) error {
	if _, err := os.Stat(spec.ComposePath); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", spec.Namespace, "-f", spec.ComposePath, "down", "-v", "--remove-orphans")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose destroy failed: %w: %s", err, string(out))
	}
	return nil
}

func (r Runtime) Status(ctx context.Context, spec ports.RuntimeSpec) ports.RuntimeStatus {
	return ports.RuntimeStatus{DockerAvailable: r.DockerAvailable(ctx), Neo4jRunning: r.Neo4jRunning(ctx, spec)}
}

func (Runtime) GraphStats(ctx context.Context) (domain.GraphStats, error) {
	return domain.GraphStats{}, nil
}

func writeCompose(spec ports.RuntimeSpec) error {
	if err := os.MkdirAll(filepath.Dir(spec.ComposePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(spec.ComposePath, []byte(renderCompose(spec)), 0o644)
}

func renderCompose(spec ports.RuntimeSpec) string {
	auth := spec.Neo4jUser + "/" + spec.Neo4jPassword
	yml := `services:
  neo4j:
    image: neo4j:5-community
    container_name: ${CONTAINER}
    ports:
      - "${HTTP_PORT}:7474"
      - "${BOLT_PORT}:7687"
    environment:
      NEO4J_AUTH: ${AUTH}
    volumes:
      - ${VOLUME}:/data
volumes:
  ${VOLUME}:
`
	replacements := map[string]string{
		"${CONTAINER}": spec.ContainerName,
		"${HTTP_PORT}": fmt.Sprintf("%d", spec.Neo4jHTTPPort),
		"${BOLT_PORT}": fmt.Sprintf("%d", spec.Neo4jBoltPort),
		"${AUTH}":      auth,
		"${VOLUME}":    spec.VolumeName,
	}
	for from, to := range replacements {
		yml = strings.ReplaceAll(yml, from, to)
	}
	return yml
}
