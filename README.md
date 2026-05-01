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
go install github.com/agent-brain/agent-brain/cmd/agent-brain@latest
```

From source:

```sh
make build
```

## Quickstart

```sh
agent-brain init
agent-brain up
agent-brain index --repo .
agent-brain context --task .ai/tasks/TICKET.md
```

Neo4j runs locally at [http://localhost:7474](http://localhost:7474) with user `neo4j` and password `agentbrain`.

## Flow With Codex/Cursor

1. Create a task in `.ai/tasks/TICKET.md`.
2. Run `agent-brain index --repo .`.
3. Run `agent-brain context --task .ai/tasks/TICKET.md`.
4. Ask the agent to read `.ai/context/<TASK_ID>.agent.md` before editing.
5. After implementation, run `agent-brain review-diff`.
6. Generate memory with `agent-brain memory-proposal --task .ai/tasks/TICKET.md`.

## Commands

- `agent-brain init`: creates `.agent-brain/` and `.ai/` working directories.
- `agent-brain up`: verifies Docker and starts Neo4j through Docker Compose.
- `agent-brain down`: stops local services without deleting data.
- `agent-brain status`: prints Docker, Neo4j, SQLite, initialization, index, and graph stats.
- `agent-brain index --repo .`: indexes a Go repository into SQLite and Neo4j.
- `agent-brain context --task .ai/tasks/TICKET.md`: writes Markdown and JSON context packs.
- `agent-brain impact --topic "text"`: searches graph impact.
- `agent-brain review-plan --plan path/to/plan.md`: reviews an agent plan.
- `agent-brain review-diff`: reviews the current git diff without modifying files.
- `agent-brain memory-proposal --task .ai/tasks/TICKET.md`: writes a structured memory proposal.
- `agent-brain memory-apply <proposal.yml>`: validates and applies memory after confirmation.

## Security

`agent-brain` is safe by default:

- It does not modify target repository code during indexing.
- It does not run commit, push, merge, reset, or destructive git commands.
- It refuses to index known secret paths such as `.env`, `.env.*`, `*.pem`, `*.key`, `credentials`, `secrets`, `kubeconfig`, `id_rsa`, and `id_ed25519`.
- It avoids printing sensitive values.

## Dependencies

- Cobra: stable CLI command framework.
- modernc.org/sqlite: SQLite driver for `database/sql` without CGO.
- neo4j-go-driver: official Neo4j Go driver.
- yaml.v3: small YAML parser for local rules and config.

## Roadmap

- Qdrant/vector DB for semantic retrieval.
- MCP server adapter.
- Web dashboard.
- Desktop app.
- Multi-repo workspace support.
- CI integration.
