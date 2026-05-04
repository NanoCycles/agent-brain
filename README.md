# agent-brain

`agent-brain` is a local CLI for AI coding agents. It indexes a repository, stores local metadata in SQLite, builds a technical graph in Neo4j, and generates compact context packs so tools like Codex and Cursor can work with fewer tokens and better engineering judgment.

The mental model is simple: the graph guides the agent, but code remains the source of truth.

## Why Go

Go is used because it gives `agent-brain` a portable single binary, straightforward CLI distribution, excellent Docker/process support, and first-class parsing of Go code through the standard library. The internal design follows a clean/hexagonal architecture so future adapters, including an MCP server, can be added without rewriting the core.

## Requirements

- Docker running locally.
- Go 1.22+ for development.
- No cloud services are required.

## Install

```sh
go install github.com/NanoCycles/agent-brain/cmd/agent-brain@latest
```

From source:

```sh
make build
```

From GitHub Releases:

```sh
curl -fsSL https://raw.githubusercontent.com/NanoCycles/agent-brain/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
iwr https://raw.githubusercontent.com/NanoCycles/agent-brain/main/scripts/install.ps1 -UseB | iex
```

Windows with Chocolatey:

```powershell
iwr https://raw.githubusercontent.com/NanoCycles/agent-brain/main/scripts/install-choco.ps1 -UseB | iex
```

After the Chocolatey community package is approved, installation becomes:

```powershell
choco install agent-brain -y
```

With npm:

```sh
npm install -g @nanocycles/agent-brain
```

## Quickstart

```sh
agent-brain prepare --task .ai/tasks/TICKET.md
```

For Jira-driven work:

```sh
agent-brain jira import https://your-site.atlassian.net/browse/AK-123
agent-brain prepare --task .ai/tasks/AK-123.md
```

Set `JIRA_BASE_URL`, `JIRA_EMAIL`, and `JIRA_API_TOKEN` to import Jira content through the REST API. Without credentials, `jira import` can create a safe local task shell with `--offline`.

Neo4j runs locally with user `neo4j` and password `agentbrain`. Each initialized repository gets its own `project_id`, Docker Compose project, Neo4j container, persistent volume, and SQLite database under `.agent-brain/runtime/`, so local projects do not share graph or metadata state. Check the exact HTTP/Bolt ports with `agent-brain status`.

The default context budget is `cavernicola`: minimal tokens, top-ranked files only, compact risks/tests/strategy, and no long prose. Use `--budget standard` or `--budget deep` only when the agent truly needs more context.

## Flow With Codex/Cursor

Agents should also read `AGENTS.md` in this repository. It is the compact operating contract for coding agents using `agent-brain`.

1. Create a task in `.ai/tasks/TICKET.md`.
2. Run `agent-brain prepare --task .ai/tasks/TICKET.md`.
3. Ask the agent to read `.ai/context/<TASK_ID>.agent.md` before editing, or paste the prompt printed by `agent-brain prepare`.
5. After implementation, run `agent-brain review-diff`.
6. Generate memory with `agent-brain memory-proposal --task .ai/tasks/TICKET.md`.

## MCP Integration

`agent-brain` can run as a local stdio MCP server so coding agents can request compact project context directly instead of spending tokens exploring the whole repository.

For Codex Desktop/CLI on this machine:

```sh
agent-brain mcp install-codex
```

This updates `~/.codex/config.toml`, creates a timestamped backup when the file already exists, and points Codex at `agent-brain mcp serve`.

Other supported local agent configs:

```sh
agent-brain mcp install-claude
agent-brain mcp install-cursor
agent-brain mcp install-copilot
```

`install-copilot` writes workspace config to `.vscode/mcp.json` so it is explicit per project. All installers create backups before replacing existing files.

```json
{
  "mcpServers": {
    "agent-brain": {
      "command": "agent-brain",
      "args": ["mcp", "serve"],
      "cwd": "/absolute/path/to/your/project"
    }
  }
}
```

On Windows, use the installed executable path if `agent-brain` is not on `PATH`:

```json
{
  "mcpServers": {
    "agent-brain": {
      "command": "C:\\tools\\agent-brain.exe",
      "args": ["mcp", "serve"],
      "cwd": "C:\\work\\your-project"
    }
  }
}
```

Recommended agent flow:

1. Call `start_task` with `task_path` or `topic` at the start of a task.
2. Read the returned handoff, generated context pack, and approved system memory.
3. Use `impact` for focused follow-up questions.
4. Use `review_diff` before finalizing changes.
5. Call `finish_task` after validation. It reviews the diff and proposes implementation/domain memory for human approval.

The MCP server exposes these tools: `start_task`, `finish_task`, `prepare_context`, `get_context_pack`, `impact`, `review_diff`, `status`, `doctor`, `memory_proposal`, `propose_domain_memory`, `apply_domain_memory`, `get_system_memory`, and `handoff`.

Project information updates when `prepare_context` runs, unless `no_index` is true. With `fast` enabled, indexing is skipped when the existing index is recent. Rules and applied memory remain local under `.agent-brain/` and `.ai/`, so context improves over time without using cloud services.

## Commands

- `agent-brain init`: creates `.agent-brain/` and `.ai/` working directories.
- `agent-brain up`: verifies Docker and starts Neo4j through Docker Compose.
- `agent-brain down`: stops local services without deleting data.
- `agent-brain destroy --confirm`: removes this project's local Neo4j volume and SQLite metadata without touching source code, config, rules, context, or memory proposals.
- `agent-brain status`: prints Docker, Neo4j, SQLite, initialization, index, and graph stats.
- `agent-brain doctor`: diagnoses Docker, Neo4j, SQLite, graph, and project wiring.
- `agent-brain logs --tail 120`: prints Neo4j runtime logs for the current project.
- `agent-brain prepare --task .ai/tasks/TICKET.md`: initializes, starts services, indexes, generates context, and prints an agent handoff prompt.
- `agent-brain prepare --topic "text"`: creates a lightweight task from topic text and prepares context.
- `agent-brain prepare --budget cavernicola|compact|standard|deep`: controls how much context is returned; default is `cavernicola`.
- `agent-brain prepare --fast`: skips reindexing when the last index is recent.
- `agent-brain prepare --no-index`: generates context from current metadata without indexing.
- `agent-brain handoff --task .ai/tasks/TICKET.md`: prints a prompt for Codex/Cursor/Claude to use the generated context pack.
- `agent-brain mcp serve`: starts the stdio MCP server for AI coding agents.
- `agent-brain mcp install-codex`: installs the local MCP server into Codex config with a backup.
- `agent-brain mcp install-claude`: installs the local MCP server into Claude Desktop config with a backup.
- `agent-brain mcp install-cursor`: installs the local MCP server into Cursor config with a backup.
- `agent-brain mcp install-copilot`: installs workspace MCP config into `.vscode/mcp.json` for GitHub Copilot.
- `agent-brain jira import AK-123`: imports a Jira issue into `.ai/tasks/AK-123.md`.
- `agent-brain index --repo .`: indexes a Go repository into SQLite and Neo4j.
- `agent-brain index --repo . --incremental`: skips graph rewrite when indexed file hashes did not change.
- `agent-brain context --task .ai/tasks/TICKET.md`: writes Markdown and JSON context packs.
- `agent-brain impact --topic "text"`: searches graph impact.
- `agent-brain review-plan --plan path/to/plan.md`: reviews an agent plan.
- `agent-brain review-comments --file review-comments.md`: turns external review comments into a prioritized agent repair plan.
- `agent-brain review-diff`: reviews the current git diff without modifying files.
- `agent-brain memory-proposal --task .ai/tasks/TICKET.md`: writes a structured memory proposal.
- `agent-brain memory-apply <proposal.yml>`: validates and applies memory after confirmation.
- `agent-brain memory-domain propose --task .ai/tasks/TICKET.md --area graphql`: proposes system/business memory by area.
- `agent-brain memory-domain apply .ai/memory-proposals/TICKET.domain.yml --yes`: applies approved domain memory to file, SQLite, and Neo4j.
- `agent-brain memory-domain list --area graphql --topic "nested count"`: prints compact approved system memory.

## Memory Model

Memory is local and explicit. A proposal is first written to `.ai/memory-proposals/` and is not trusted until it is confirmed. Implementation memory captures what changed for a task. Domain memory captures how the system works: concepts, components, business rules, and invariants, grouped by areas such as `graphql`, `auth`, `billing`, `events`, and `persistence`.

Applied memory is stored in SQLite as the local audit/source-of-truth record and mirrored into Neo4j as technical/business graph nodes so future impact/context queries can use it. Source code remains the source of truth for implementation details.

## Security

`agent-brain` is safe by default:

- It does not modify target repository code during indexing.
- It does not run commit, push, merge, reset, or destructive git commands.
- It refuses to index known secret paths such as `.env`, `.env.*`, `*.pem`, `*.key`, `credentials`, `secrets`, `kubeconfig`, `id_rsa`, and `id_ed25519`.
- It avoids printing sensitive values.
- The default Neo4j password is for local development only. Do not expose the generated Neo4j ports to untrusted networks.

## Dependencies

- Cobra: stable CLI command framework.
- modernc.org/sqlite: SQLite driver for `database/sql` without CGO.
- neo4j-go-driver: official Neo4j Go driver.
- golang.org/x/tools/go/packages: typed Go package loading for more accurate Go call graph and interface implementation analysis.
- yaml.v3: small YAML parser for local rules and config.

The Node/JavaScript/TypeScript indexer intentionally uses lightweight built-in parsing heuristics for MVP portability: imports, exported functions, classes, TypeScript interfaces, tests, REST route registrations, GraphQL resolver-like files, and event files are indexed without adding a JS parser dependency.

## Publishing Releases

Public binaries are produced with GoReleaser through GitHub Actions.

```sh
git tag v0.1.0
git push origin v0.1.0
```

The release workflow runs tests and publishes Linux, macOS, Windows archives, checksums, installer scripts, a Chocolatey `.nupkg`, and an npm package tarball. If `CHOCOLATEY_API_KEY` or `NPM_TOKEN` repository secrets are configured, the workflow also pushes to Chocolatey Community and npm.

## Roadmap

- Qdrant/vector DB for semantic retrieval.
- Deeper MCP resources/prompts and streaming progress.
- Web dashboard.
- Desktop app.
- Multi-repo workspace support.
- CI integration.
