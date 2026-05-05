package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type MCPInstallResult struct {
	ConfigPath string
	BackupPath string
	Command    string
	Changed    bool
}

func InstallCodexMCP() (MCPInstallResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return MCPInstallResult{}, err
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	command := detectAgentBrainCommand()
	repoRoot, _ := os.Getwd()
	section := codexMCPSection(command, repoRoot)

	var existing string
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return MCPInstallResult{}, err
	}

	next := upsertTOMLSection(existing, "[mcp_servers.agent-brain]", section)
	result := MCPInstallResult{ConfigPath: configPath, Command: command, Changed: next != existing}
	if !result.Changed {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return MCPInstallResult{}, err
	}
	if existing != "" {
		result.BackupPath = configPath + ".bak-agent-brain-" + time.Now().UTC().Format("20060102150405")
		if err := os.WriteFile(result.BackupPath, []byte(existing), 0o644); err != nil {
			return MCPInstallResult{}, err
		}
	}
	if err := os.WriteFile(configPath, []byte(next), 0o644); err != nil {
		return MCPInstallResult{}, err
	}
	return result, nil
}

func InstallClaudeMCP() (MCPInstallResult, error) {
	configPath, err := claudeConfigPath()
	if err != nil {
		return MCPInstallResult{}, err
	}
	return installJSONMCP(configPath)
}

func InstallCursorMCP() (MCPInstallResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return MCPInstallResult{}, err
	}
	return installJSONMCP(filepath.Join(home, ".cursor", "mcp.json"))
}

func InstallCopilotMCP(workspaceRoot string) (MCPInstallResult, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return MCPInstallResult{}, err
		}
	}
	return installJSONMCP(filepath.Join(workspaceRoot, ".vscode", "mcp.json"))
}

func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
	}
}

func installJSONMCP(configPath string) (MCPInstallResult, error) {
	command := detectAgentBrainCommand()
	repoRoot, _ := os.Getwd()
	var existing []byte
	var root map[string]any
	if data, err := os.ReadFile(configPath); err == nil {
		existing = data
		_ = json.Unmarshal(data, &root)
	} else if !os.IsNotExist(err) {
		return MCPInstallResult{}, err
	}
	if root == nil {
		root = map[string]any{}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["agent-brain"] = map[string]any{
		"command": command,
		"args":    []string{"mcp", "serve"},
		"cwd":     repoRoot,
		"env": map[string]string{
			"AGENT_BRAIN_REPO_ROOT": repoRoot,
		},
	}
	root["mcpServers"] = servers
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return MCPInstallResult{}, err
	}
	next = append(next, '\n')
	result := MCPInstallResult{ConfigPath: configPath, Command: command, Changed: string(existing) != string(next)}
	if !result.Changed {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return MCPInstallResult{}, err
	}
	if len(existing) > 0 {
		result.BackupPath = configPath + ".bak-agent-brain-" + time.Now().UTC().Format("20060102150405")
		if err := os.WriteFile(result.BackupPath, existing, 0o644); err != nil {
			return MCPInstallResult{}, err
		}
	}
	if err := os.WriteFile(configPath, next, 0o644); err != nil {
		return MCPInstallResult{}, err
	}
	return result, nil
}

func detectAgentBrainCommand() string {
	name := "agent-brain"
	if runtime.GOOS == "windows" {
		name = "agent-brain.cmd"
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if path, err := exec.LookPath("agent-brain"); err == nil {
		return path
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "agent-brain"
}

func codexMCPSection(command, repoRoot string) string {
	if repoRoot == "" {
		repoRoot = "."
	}
	return fmt.Sprintf("[mcp_servers.agent-brain]\nenabled = true\ncommand = %q\nargs = [\"mcp\", \"serve\"]\nenv = { AGENT_BRAIN_REPO_ROOT = %q }\n", command, repoRoot)
}

func upsertTOMLSection(content, header, section string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i
			break
		}
	}
	if start < 0 {
		content = strings.TrimRight(content, "\n")
		if content != "" {
			content += "\n\n"
		}
		return content + strings.TrimRight(section, "\n") + "\n"
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}
	replacement := strings.Split(strings.TrimRight(section, "\n"), "\n")
	next := append([]string{}, lines[:start]...)
	next = append(next, replacement...)
	next = append(next, lines[end:]...)
	return strings.TrimRight(strings.Join(next, "\n"), "\n") + "\n"
}
