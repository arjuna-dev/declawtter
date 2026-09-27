# AGENTS.md

This repository builds the `declaw` CLI.

## Purpose

`declaw` manages assistant workspaces, tracked projects, interactive agent checkouts, recent-conversation checkout across harnesses, and macOS launchd schedules for Codex jobs.

Supported harnesses: Pi, Hermes, Codex, Claude, OpenCode, and Antigravity.

## Important Structure

- `cmd/declaw/`: CLI entrypoint.
- `internal/app/`: top-level command routing and interactive launcher integration.
- `internal/projects/`: project registry, create/track/path/remove behavior.
- `internal/activity/`: harness detection and recent-activity discovery across harness data stores.
- `internal/conversations/`: per-harness chat discovery, portable transcripts, and chat pin/ignore/alias state.
- `internal/scheduler/`: launchd scheduling, Codex job management, and run handoff.
- `internal/ui/`: Bubble Tea command launcher.
- `internal/workspacetemplate/`: embedded workspace template used by `declaw create`.
- `internal/agentworkspace/`: embedded workspace used by `declaw ai-agent`.

The root repository is not itself a generated assistant workspace. Do not expect a root `WORKSPACE/` directory here.

## Development Rules

- Keep non-interactive CLI commands stable for LLM agents.
- `declaw checkout <name>` without the `project` keyword is a deprecated alias kept for existing agent scripts; do not remove it.
- Keep the three AGENTS.md files distinct: this root file is development-only and never ships, while `internal/agentworkspace/template/AGENTS.md` and `internal/workspacetemplate/template/AGENTS.md` are embedded in the binary and must be updated when user-facing commands change.
- Put user-facing workflow changes in `declaw -h` / command help where agents can discover them.
- Keep generated workspace content inside `internal/workspacetemplate/template`.
- Keep declaw-management agent instructions inside `internal/agentworkspace/template/AGENTS.md`.
- Rebuild the installed binary after CLI changes with `go build -o ~/.local/bin/declaw ./cmd/declaw`.
- Run `go test ./...` before finishing code changes.

## Scheduling Policy

- Recurring Codex schedules require `--project <name>`.
- If an existing directory needs recurring schedules, first use `declaw track <name> --path <dir>`.
- One-off Codex schedules may use `--project`, `--workspace`, or omit both to use the default one-off workspace.
- Use Codex schedules only when future Codex work should run.

## Safety

- Do not manually edit launchd files or project registries unless the CLI path is broken and the user agrees.
- Treat every harness session store (Codex rollouts, Claude projects, Pi, Hermes, OpenCode and Antigravity databases) as strictly read-only. These formats are private and undocumented, they change without notice, and writing into them can corrupt real user history. Cross-harness checkout therefore renders a portable transcript and injects it as a prompt rather than writing another harness's format.
- Declaw's own chat state (pin, ignore, alias) belongs in declaw's directory, never inside a harness store.
- Do not use destructive git commands.
- Do not remove user-created tracked project directories unless the user explicitly asks.
