package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertTOMLSectionAddsCodexMCP(t *testing.T) {
	got := upsertTOMLSection("[mcp_servers.jira]\nenabled = true\n", "[mcp_servers.agent-brain]", codexMCPSection("agent-brain"))
	if !strings.Contains(got, "[mcp_servers.agent-brain]") {
		t.Fatalf("missing agent-brain section: %s", got)
	}
	if !strings.Contains(got, `args = ["mcp", "serve"]`) {
		t.Fatalf("missing mcp serve args: %s", got)
	}
}

func TestUpsertTOMLSectionReplacesExistingCodexMCP(t *testing.T) {
	input := "[mcp_servers.agent-brain]\nenabled = false\ncommand = \"old\"\n\n[mcp_servers.jira]\nenabled = true\n"
	got := upsertTOMLSection(input, "[mcp_servers.agent-brain]", codexMCPSection("agent-brain"))
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
}
