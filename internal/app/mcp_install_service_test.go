package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertTOMLSectionAddsCodexMCP(t *testing.T) {
	got := upsertTOMLSection("[mcp_servers.jira]\nenabled = true\n", "[mcp_servers.agent-brain]", codexMCPSection("agent-brain", "C:\\repo"))
	if !strings.Contains(got, "[mcp_servers.agent-brain]") {
		t.Fatalf("missing agent-brain section: %s", got)
	}
	if !strings.Contains(got, `args = ["mcp", "serve"]`) {
		t.Fatalf("missing mcp serve args: %s", got)
	}
	if !strings.Contains(got, "AGENT_BRAIN_REPO_ROOT") {
		t.Fatalf("missing repo root env: %s", got)
	}
}

func TestUpsertTOMLSectionReplacesExistingCodexMCP(t *testing.T) {
	input := "[mcp_servers.agent-brain]\nenabled = false\ncommand = \"old\"\n\n[mcp_servers.jira]\nenabled = true\n"
	got := upsertTOMLSection(input, "[mcp_servers.agent-brain]", codexMCPSection("agent-brain", "C:\\repo"))
	if strings.Contains(got, `command = "old"`) || strings.Contains(got, "enabled = false") {
		t.Fatalf("old section was not replaced: %s", got)
	}
	if strings.Count(got, "[mcp_servers.agent-brain]") != 1 {
		t.Fatalf("expected one agent-brain section: %s", got)
	}
}

func TestInstallJSONMCPShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	result, err := installJSONMCP(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ConfigPath != path || !result.Changed {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mcpServers"`) || !strings.Contains(string(data), `"agent-brain"`) {
		t.Fatalf("missing mcp server config: %s", data)
	}
	if !strings.Contains(string(data), `"AGENT_BRAIN_REPO_ROOT"`) || !strings.Contains(string(data), `"cwd"`) {
		t.Fatalf("missing repo binding in mcp server config: %s", data)
	}
}
