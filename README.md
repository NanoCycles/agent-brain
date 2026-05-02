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

Neo4j runs locally with user `neo4j` and password `agentbrain`. Each initialized repository gets its own `project_id`, Docker Compose project, Neo4j container, persistent volume, and SQLite database under `.agent-brain/runtime/`, so local projects do not share graph or metadata state. Check the exact HTTP/Bolt ports with `agent-brain status`.

The default context budget is `cavernicola`: minimal tokens, top-ranked files only, compact risks/tests/strategy, and no long prose. Use `--budget standard` or `--budget deep` only when the agent truly needs more context.

## Flow With Codex/Cursor

1. Create a task in `.ai/tasks/TICKET.md`.
2. Run `agent-brain prepare --task .ai/tasks/TICKET.md`.
3. Ask the agent to read `.ai/context/<TASK_ID>.agent.md` before editing, or paste the prompt printed by `agent-brain prepare`.
5. After implementation, run `agent-brain review-diff`.
6. Generate memory with `agent-brain memory-proposal --task .ai/tasks/TICKET.md`.

## MCP Integration

`agent-brain` can run as a local stdio MCP server so coding agents can request compact project context directly instead of spending tokens exploring the whole repository.

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

1. Call `prepare_context` with `task_path` or `topic` at the start of a task. Default `budget` is `cavernicola`.
2. Read the returned handoff and generated context pack.
3. Use `impact` for focused follow-up questions.
4. Use `review_diff` before finalizing changes.
5. Use `memory_proposal` after a bug or feature is solved; memory is not applied automatically.

The MCP server exposes these tools: `prepare_context`, `get_context_pack`, `impact`, `review_diff`, `status`, `memory_proposal`, and `handoff`.

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
- `agent-brain index --repo .`: indexes a Go repository into SQLite and Neo4j.
- `agent-brain context --task .ai/tasks/TICKET.md`: writes Markdown and JSON context packs.
- `agent-brain impact --topic "text"`: searches graph impact.
- `agent-brain review-plan --plan path/to/plan.md`: reviews an agent plan.
- `agent-brain review-diff`: reviews the current git diff without modifying files.
- `agent-brain memory-proposal --task .ai/tasks/TICKET.md`: writes a structured memory proposal.
- `agent-brain memory-apply <proposal.yml>`: validates and applies memory after confirmation.

## Memory Model

Memory is local and explicit. A proposal is first written to `.ai/memory-proposals/` and is not trusted until `memory-apply` is confirmed. Applied memory is stored in SQLite as the local audit/source-of-truth record and mirrored into Neo4j as `Task`, `Memory`, `Rule`, and `Risk` nodes so future impact/context queries can use it. Source code remains the source of truth for implementation details.

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
- golang.org/x/tools/go/packages: typed Go package loading for more accurate call graph and interface implementation analysis.
- yaml.v3: small YAML parser for local rules and config.

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
