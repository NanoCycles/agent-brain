package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type Runtime struct{}

func DockerCLIPath() (string, error) {
	return dockerExecutable()
}

func Command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	return dockerCommand(ctx, args...)
}

func (Runtime) DockerAvailable(ctx context.Context) bool {
	return dockerInfo(ctx) == nil
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
	if err := r.waitForDocker(ctx, 90*time.Second); err != nil {
		return err
	}
	if err := writeCompose(spec); err != nil {
		return err
	}
	cmd, err := dockerCommand(ctx, "compose", "-p", spec.Namespace, "-f", spec.ComposePath, "up", "-d")
	if err != nil {
		return err
	}
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
	cmd, err := dockerCommand(ctx, "compose", "-p", spec.Namespace, "-f", spec.ComposePath, "down")
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose down failed: %w: %s", err, string(out))
	}
	return nil
}

func (Runtime) Destroy(ctx context.Context, spec ports.RuntimeSpec) error {
	if _, err := os.Stat(spec.ComposePath); err != nil {
		return err
	}
	cmd, err := dockerCommand(ctx, "compose", "-p", spec.Namespace, "-f", spec.ComposePath, "down", "-v", "--remove-orphans")
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose destroy failed: %w: %s", err, string(out))
	}
	return nil
}

func (r Runtime) Status(ctx context.Context, spec ports.RuntimeSpec) ports.RuntimeStatus {
	return ports.RuntimeStatus{DockerAvailable: r.DockerAvailable(ctx), Neo4jRunning: r.Neo4jRunning(ctx, spec)}
}

func (r Runtime) waitForDocker(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := dockerInfo(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("docker is not available to this agent process: %w. Start Docker Desktop, then restart the IDE/agent MCP host or run agent-brain mcp install-* again from the target repo", lastErr)
}

func (Runtime) GraphStats(ctx context.Context) (domain.GraphStats, error) {
	return domain.GraphStats{}, nil
}

func dockerInfo(ctx context.Context) error {
	cmd, err := dockerCommand(ctx, "info")
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dockerCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	exe, err := dockerExecutable()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, exe, args...), nil
}

func dockerExecutable() (string, error) {
	if path := strings.TrimSpace(os.Getenv("AGENT_BRAIN_DOCKER")); path != "" {
		if fileExists(path) {
			return path, nil
		}
	}
	if path, err := exec.LookPath("docker"); err == nil {
		return path, nil
	}
	for _, path := range dockerCandidatePaths() {
		if fileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("docker CLI not found in PATH or known Docker Desktop locations")
}

func dockerCandidatePaths() []string {
	if runtime.GOOS != "windows" {
		return []string{"/usr/local/bin/docker", "/opt/homebrew/bin/docker", "/usr/bin/docker"}
	}
	var paths []string
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432")} {
		if root != "" {
			paths = append(paths, filepath.Join(root, "Docker", "Docker", "resources", "bin", "docker.exe"))
		}
	}
	if root := os.Getenv("LOCALAPPDATA"); root != "" {
		paths = append(paths, filepath.Join(root, "Docker", "resources", "bin", "docker.exe"))
	}
	paths = append(paths, `C:\Program Files\Docker\Docker\resources\bin\docker.exe`)
	return uniqueStrings(paths)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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
