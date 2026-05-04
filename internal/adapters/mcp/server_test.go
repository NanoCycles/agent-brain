package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
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
	text := server.startAsync(context.Background(), "test_operation", nil, func(ctx context.Context) (string, error) {
		defer close(done)
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
}

func TestAsyncOperationCancel(t *testing.T) {
	server := NewServer(strings.NewReader(""), &bytes.Buffer{})
	text := server.startAsync(context.Background(), "test_operation", nil, func(ctx context.Context) (string, error) {
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
