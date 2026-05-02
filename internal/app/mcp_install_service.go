package app

import (
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
	section := codexMCPSection(command)

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

func codexMCPSection(command string) string {
	return fmt.Sprintf("[mcp_servers.agent-brain]\nenabled = true\ncommand = %q\nargs = [\"mcp\", \"serve\"]\n", command)
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
