# Agent Operating Guide

This repository is designed for AI coding agents first. Keep token usage low and use `agent-brain` as the local source of project context, technical rules, graph impact, and approved system memory.

## Required Flow

1. Before editing code, call the `agent-brain` MCP tool `start_task` with `task_path` or `topic`.
2. Read the generated context pack and approved system memory.
3. Open only the top-ranked files first.
4. Use `impact` only for focused follow-up questions.
5. Do not broadly scan the repository unless context quality is low and the human approves broader exploration.
6. Implement the smallest safe change.
7. Run focused tests, then broader tests when risk or touched surface requires it.
8. Before final response, call `review_diff`.
9. After validation, call `finish_task` to generate implementation and domain memory proposals.
10. Do not apply memory automatically. Ask for human approval before `apply_domain_memory` or memory apply commands.

## Safety Rules

- Do not commit, push, merge, reset, or run destructive git commands unless the human explicitly asks.
- Do not modify public contracts, schemas, routes, protobufs, events, or DB behavior without explicit approval.
- Do not expose secrets or index forbidden files.
- Preserve tenant/project isolation and authorization behavior.
- Add or adjust regression tests for bugs.
- Keep adapters thin and business logic out of transport layers.

## Token Budget

Default to the smallest useful context:

- Use `budget = "cavernicola"` for normal tasks.
- Ask before switching to `standard` or `deep`.
- Prefer generated context, graph impact, rules, and system memory over raw repository scanning.

## Domain Memory

Domain memory captures how the target system works, not just how a task was fixed. Useful memory includes:

- system concepts
- component responsibilities
- business rules
- invariants
- contract semantics
- authorization and isolation rules
- known pitfalls

Use areas when proposing or reading memory:

- `graphql`
- `auth`
- `billing`
- `events`
- `persistence`
- `application`
- `domain`

The source code remains the source of truth. Memory is guidance for faster, safer agent reasoning.
