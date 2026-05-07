package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type AgentSkillPack struct {
	Target string
	Path   string
}

type AgentSkillInstallResult struct {
	Target     string
	Path       string
	BackupPath string
	Changed    bool
}

func GenerateAgentSkillPacks(outputDir string) ([]AgentSkillPack, error) {
	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Join(".agent-brain", "agent-skills")
	}
	definitions := map[string]map[string]string{
		"codex": {
			filepath.Join("agent-brain", "SKILL.md"): codexSkillMarkdown(),
		},
		"cursor": {
			filepath.Join("rules", "agent-brain.mdc"): cursorRuleMarkdown(),
		},
		"claude": {
			"CLAUDE.agent-brain.md": claudeGuideMarkdown(),
		},
		"copilot": {
			filepath.Join("instructions", "agent-brain.instructions.md"): copilotInstructionsMarkdown(),
		},
		"antigravity": {
			"agent-brain.md": antigravityGuideMarkdown(),
		},
		"generic-mcp": {
			"agent-brain-mcp.md": genericMCPGuideMarkdown(),
		},
	}
	var packs []AgentSkillPack
	for target, files := range definitions {
		for rel, content := range files {
			path := filepath.Join(outputDir, target, rel)
			if err := writeIfChanged(path, content); err != nil {
				return nil, err
			}
			packs = append(packs, AgentSkillPack{Target: target, Path: path})
		}
	}
	return packs, nil
}

func InstallCodexSkill() (AgentSkillInstallResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AgentSkillInstallResult{}, err
	}
	path := filepath.Join(home, ".codex", "skills", "agent-brain", "SKILL.md")
	return installSkillFile("codex", path, codexSkillMarkdown())
}

func InstallCursorRules(workspaceRoot string) (AgentSkillInstallResult, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return AgentSkillInstallResult{}, err
		}
	}
	path := filepath.Join(workspaceRoot, ".cursor", "rules", "agent-brain.mdc")
	return installSkillFile("cursor", path, cursorRuleMarkdown())
}

func InstallClaudeGuide(workspaceRoot string) (AgentSkillInstallResult, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return AgentSkillInstallResult{}, err
		}
	}
	path := filepath.Join(workspaceRoot, "CLAUDE.agent-brain.md")
	return installSkillFile("claude", path, claudeGuideMarkdown())
}

func InstallCopilotInstructions(workspaceRoot string) (AgentSkillInstallResult, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return AgentSkillInstallResult{}, err
		}
	}
	path := filepath.Join(workspaceRoot, ".github", "instructions", "agent-brain.instructions.md")
	return installSkillFile("copilot", path, copilotInstructionsMarkdown())
}

func InstallAntigravityGuide(workspaceRoot string) (AgentSkillInstallResult, error) {
	if workspaceRoot == "" {
		var err error
		workspaceRoot, err = os.Getwd()
		if err != nil {
			return AgentSkillInstallResult{}, err
		}
	}
	path := filepath.Join(workspaceRoot, ".agent-brain", "agent-skills", "antigravity", "agent-brain.md")
	return installSkillFile("antigravity", path, antigravityGuideMarkdown())
}

func installSkillFile(target, path, content string) (AgentSkillInstallResult, error) {
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return AgentSkillInstallResult{}, err
	}
	next := []byte(strings.TrimRight(content, "\n") + "\n")
	result := AgentSkillInstallResult{Target: target, Path: path, Changed: string(existing) != string(next)}
	if !result.Changed {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return AgentSkillInstallResult{}, err
	}
	if len(existing) > 0 {
		result.BackupPath = path + ".bak-agent-brain"
		if err := os.WriteFile(result.BackupPath, existing, 0o644); err != nil {
			return AgentSkillInstallResult{}, err
		}
	}
	if err := os.WriteFile(path, next, 0o644); err != nil {
		return AgentSkillInstallResult{}, err
	}
	return result, nil
}

func writeIfChanged(path, content string) error {
	next := []byte(strings.TrimRight(content, "\n") + "\n")
	if current, err := os.ReadFile(path); err == nil && string(current) == string(next) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, next, 0o644)
}

func codexSkillMarkdown() string {
	return `---
name: agent-brain
description: Use the local agent-brain MCP server before editing code to get compact repository context, approved domain memory, impact analysis, and review gates. Use for coding tasks, bug fixes, features, reviews, and external review comment repair.
---

# agent-brain

Use this skill when working in a repository that has ` + "`agent-brain`" + ` installed or configured as an MCP server.

## Required Flow

1. Call ` + "`change_start`" + ` with ` + "`task_path`" + ` or ` + "`topic`" + ` before reading broad code.
2. Use ` + "`budget = \"cavernicola\"`" + ` by default.
3. Read the generated context pack, approved system memory, change scope, and implementation checklist.
4. Open only top-ranked files first.
5. For large or critical changes, write a short plan and call ` + "`plan_gate`" + ` before broad edits.
6. Use ` + "`impact`" + ` only for focused follow-up questions.
7. Use ` + "`change_checkpoint`" + ` after each logical edit batch.
8. Run focused tests, then required broader tests.
9. Call ` + "`review_simulate`" + ` and ` + "`review_diff`" + ` before final response.
10. Call ` + "`change_finish`" + ` after validation to generate memory proposals.

Never apply memory automatically. Ask the human before ` + "`apply_domain_memory`" + ` or memory apply commands.`
}

func cursorRuleMarkdown() string {
	return `---
description: agent-brain local context intelligence workflow
alwaysApply: true
---

Before editing code, use the agent-brain MCP server. Prefer ` + "`change_start`" + ` with ` + "`budget = \"cavernicola\"`" + `. Read the context pack, approved system memory, change scope, and implementation checklist. Open only top-ranked files first.

For large or critical changes, create a short plan and call ` + "`plan_gate`" + `. During implementation, call ` + "`change_checkpoint`" + ` after each logical batch. Before final response, call ` + "`review_simulate`" + ` and ` + "`review_diff`" + `. After validation, call ` + "`change_finish`" + ` and ask before applying any memory proposal.

Do not modify public contracts, schemas, routes, protobufs, events, DB behavior, auth/authz, or tenant/project isolation without explicit human approval.`
}

func claudeGuideMarkdown() string {
	return `# agent-brain Claude Guide

Use agent-brain as the local source of repository context, domain memory, architecture rules, impact analysis, and review gates.

Required flow:

1. Call ` + "`change_start`" + ` with ` + "`task_path`" + ` or ` + "`topic`" + ` before broad file reads.
2. Use ` + "`budget = \"cavernicola\"`" + ` unless the human approves more context.
3. Read the context pack, approved memory, change scope, and implementation checklist.
4. Open only top-ranked files first.
5. For large/critical changes, call ` + "`plan_gate`" + ` before broad edits.
6. Run focused tests.
7. Call ` + "`review_simulate`" + ` and ` + "`review_diff`" + ` before final response.
8. Call ` + "`change_finish`" + ` after validation and ask before applying memory.
`
}

func copilotInstructionsMarkdown() string {
	return `# agent-brain Instructions

When agent-brain MCP is available, use it before code exploration.

- Start with ` + "`change_start`" + ` using the task or topic.
- Use ` + "`cavernicola`" + ` budget by default.
- Follow the generated implementation checklist.
- Use ` + "`plan_gate`" + ` for large or critical plans.
- Use ` + "`change_checkpoint`" + ` during large changes.
- Use ` + "`review_simulate`" + ` and ` + "`review_diff`" + ` before final response.
- Use ` + "`change_finish`" + ` to propose memory updates.
- Never apply memory without human approval.
`
}

func antigravityGuideMarkdown() string {
	return `# agent-brain Antigravity MCP Guide

Configure a stdio MCP server:

` + "```json" + `
{
  "mcpServers": {
    "agent-brain": {
      "command": "agent-brain",
      "args": ["mcp", "serve"],
      "cwd": "/absolute/path/to/project"
    }
  }
}
` + "```" + `

Agent workflow: call ` + "`change_start`" + ` first, use ` + "`cavernicola`" + ` budget, follow the implementation checklist, run ` + "`plan_gate`" + ` for large/critical work, then ` + "`review_simulate`" + `, ` + "`review_diff`" + `, and ` + "`change_finish`" + `.
`
}

func genericMCPGuideMarkdown() string {
	return fmt.Sprintf(`# agent-brain Generic MCP Pack

Command: %s
Args: ["mcp", "serve"]

Use tools in this order for implementation work:

1. change_start
2. plan_gate for large/critical work
3. impact for focused questions
4. change_checkpoint during large edits
5. review_simulate
6. review_diff
7. change_finish

Use cavernicola budget by default. Do not apply memory automatically.
`, detectAgentBrainCommandForSkill())
}

func detectAgentBrainCommandForSkill() string {
	if runtime.GOOS == "windows" {
		return "agent-brain.cmd"
	}
	return "agent-brain"
}
