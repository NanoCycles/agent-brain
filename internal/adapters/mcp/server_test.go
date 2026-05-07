package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NanoCycles/agent-brain/internal/app"
	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestServerInitializeAndListTools(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var output bytes.Buffer
	if err := NewServer(strings.NewReader(input), &output).Serve(context.Background()); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := nonEmptyLines(output.String())
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(lines), output.String())
	}
	var initResp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("unmarshal initialize response: %v", err)
	}
	result := initResp["result"].(map[string]any)
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "agent-brain" {
		t.Fatalf("unexpected server name: %v", serverInfo["name"])
	}
	if serverInfo["version"] != serverVersion {
		t.Fatalf("unexpected server version: %v", serverInfo["version"])
	}
	if !strings.Contains(lines[1], "prepare_context") {
		t.Fatalf("tools/list response does not include prepare_context: %s", lines[1])
	}
	if !strings.Contains(lines[1], "review_diff") {
		t.Fatalf("tools/list response does not include review_diff: %s", lines[1])
	}
	if !strings.Contains(lines[1], "prepare_context_async") {
		t.Fatalf("tools/list response does not include prepare_context_async: %s", lines[1])
	}
	if !strings.Contains(lines[1], "operation_status") {
		t.Fatalf("tools/list response does not include operation_status: %s", lines[1])
	}
	if !strings.Contains(lines[1], "operation_list") || !strings.Contains(lines[1], "clean_context") || !strings.Contains(lines[1], "mcp_health") {
		t.Fatalf("tools/list response does not include operation maintenance tools: %s", lines[1])
	}
	if !strings.Contains(lines[1], "agent_start_async") || !strings.Contains(lines[1], "bootstrap_domain_memory") {
		t.Fatalf("tools/list response does not include agent workflow tools: %s", lines[1])
	}
	if !strings.Contains(lines[1], "github_pr_comments_async") {
		t.Fatalf("tools/list response does not include GitHub async tool: %s", lines[1])
	}
	if !strings.Contains(lines[1], "change_start") || !strings.Contains(lines[1], "plan_gate") || !strings.Contains(lines[1], "review_simulate") {
		t.Fatalf("tools/list response does not include change intelligence tools: %s", lines[1])
	}
}

func TestUnknownToolReturnsToolError(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"missing","arguments":{}}}` + "\n"
	var output bytes.Buffer
	if err := NewServer(strings.NewReader(input), &output).Serve(context.Background()); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if !strings.Contains(output.String(), `"isError":true`) {
		t.Fatalf("expected tool error response, got %s", output.String())
	}
	if !strings.Contains(output.String(), "unknown tool") {
		t.Fatalf("expected unknown tool message, got %s", output.String())
	}
}

func TestAsyncOperationStatus(t *testing.T) {
	server := NewServer(strings.NewReader(""), &bytes.Buffer{})
	done := make(chan struct{})
	text := server.startAsync(context.Background(), "test_operation", "C:\\repo", nil, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
		defer close(done)
		progress(50, "halfway")
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return "ok", nil
		}
	})
	id := operationIDFromText(t, text)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("operation did not complete")
	}
	status, err := server.operationStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, `"status": "completed"`) || !strings.Contains(status, `"result": "ok"`) {
		t.Fatalf("unexpected operation status: %s", status)
	}
	if !strings.Contains(status, `"stage": "halfway"`) {
		t.Fatalf("expected detailed operation progress event: %s", status)
	}
}

func TestLongSynchronousToolDefaultsToAsyncOperation(t *testing.T) {
	root, err := os.MkdirTemp("", "agent-brain-mcp-default-async-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := 0; i < 20; i++ {
			if err := os.RemoveAll(root); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServer(strings.NewReader(""), &bytes.Buffer{})
	text, err := server.callTool(context.Background(), "start_task", map[string]any{
		"repo_root": root,
		"topic":     "investigate schema cache",
		"no_index":  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Operation started:") || !strings.Contains(text, "operation_status") {
		t.Fatalf("expected default async operation response, got %s", text)
	}
	id := operationIDFromText(t, text)
	_, _ = server.operationCancel(id)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := server.operationStatus(id)
		if err == nil && strings.Contains(status, `"cancelled"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestChangeStartDefaultsToAsyncOperation(t *testing.T) {
	root, err := os.MkdirTemp("", "agent-brain-mcp-change-start-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := 0; i < 20; i++ {
			if err := os.RemoveAll(root); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServer(strings.NewReader(""), &bytes.Buffer{})
	text, err := server.callTool(context.Background(), "change_start", map[string]any{
		"repo_root": root,
		"topic":     "GraphQL nested count bug",
		"no_index":  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Operation started:") || !strings.Contains(text, "change_start") {
		t.Fatalf("expected async change_start operation, got %s", text)
	}
	id := operationIDFromText(t, text)
	_, _ = server.operationCancel(id)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := server.operationStatus(id)
		if err == nil && strings.Contains(status, `"cancelled"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAsyncOperationCancel(t *testing.T) {
	server := NewServer(strings.NewReader(""), &bytes.Buffer{})
	text := server.startAsync(context.Background(), "test_operation", "C:\\repo", nil, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	id := operationIDFromText(t, text)
	if _, err := server.operationCancel(id); err != nil {
		t.Fatal(err)
	}
	status, err := server.operationStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, `"status": "cancelled"`) {
		t.Fatalf("unexpected operation status: %s", status)
	}
}

func TestOperationListFiltersByRepoRoot(t *testing.T) {
	server := NewServer(strings.NewReader(""), &bytes.Buffer{})
	one := operationIDFromText(t, server.startAsync(context.Background(), "one", "C:\\repo-one", nil, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
		return "one", nil
	}))
	_ = operationIDFromText(t, server.startAsync(context.Background(), "two", "C:\\repo-two", nil, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
		return "two", nil
	}))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := server.operationStatus(one)
		if err == nil && strings.Contains(status, `"completed"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	list, err := server.operationList("C:\\repo-one")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "repo-one") || strings.Contains(list, "repo-two") {
		t.Fatalf("unexpected operation list: %s", list)
	}
}

func TestDoctorActionsRecommendAsyncWhenReady(t *testing.T) {
	actions := doctorActions(app.StatusReport{DockerAvailable: true, Neo4jRunning: true, GraphStats: domain.GraphStats{Nodes: 3}}, 2, domain.RepoCapabilities{HasTests: true}, app.ContextOutputStats{GeneratedPacks: 1})
	if len(actions) != 1 || !strings.Contains(actions[0], "prepare_context_async") {
		t.Fatalf("unexpected actions: %#v", actions)
	}
}

func TestDiscoverProjectPathsPrefersRepoRootArg(t *testing.T) {
	root := t.TempDir()
	p, err := discoverProjectPaths(map[string]any{"repo_root": root})
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != root {
		t.Fatalf("expected %q, got %q", root, p.Root)
	}
}

func TestDiscoverProjectPathsUsesEnvRoot(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, t.TempDir())
	t.Setenv("AGENT_BRAIN_REPO_ROOT", root)
	p, err := discoverProjectPaths(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != root {
		t.Fatalf("expected %q, got %q", root, p.Root)
	}
}

func TestDiscoverProjectPathsPrefersProjectCWDOverEnvRoot(t *testing.T) {
	cwdRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwdRoot, ".agent-brain"), 0o755); err != nil {
		t.Fatal(err)
	}
	envRoot := t.TempDir()
	chdirForTest(t, cwdRoot)
	t.Setenv("AGENT_BRAIN_REPO_ROOT", envRoot)
	p, err := discoverProjectPaths(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != cwdRoot {
		t.Fatalf("expected cwd root %q, got %q", cwdRoot, p.Root)
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previous)
	})
}

func operationIDFromText(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Operation started: ") {
			return strings.TrimPrefix(line, "Operation started: ")
		}
	}
	t.Fatal(fmt.Sprintf("operation id not found in %q", text))
	return ""
}

func nonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
