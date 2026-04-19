# TOOLS.md

Keep local environment notes here.

This file is for workspace-specific operational details that should not live in generic skills or prompts.

## Put Things Here Like

- local hostnames and aliases
- preferred browser or terminal setup
- device names
- voice or TTS preferences
- path quirks
- local CLI assumptions such as the presence of the `codex` command

## Why This Exists

Skills are reusable. This file is local.

Use it as a cheat sheet for facts that are true in this workspace and useful across sessions.

## Local Notes

- `declaw` is the main local CLI for this workspace.
- Use `declaw checkout <project>` to open Codex in a tracked project context.
- Use `declaw schedule codex ...` for scheduled Codex jobs.
- Use `declaw schedule claude ...` for scheduled Claude Code jobs.
- Recurring agent schedules require `--project <name>`.
- If an existing directory should become a recurring agent target, use `declaw track <name> --path <dir>` first.
- One-off agent schedules may use `--project`, `--workspace`, or no target.
- Scheduled Codex jobs use declaw's app-server chat UI by default. Use `--ui declaw` for the legacy `codex exec` chat UI, and `--ui codex` only when raw Codex TUI output is wanted.
- Scheduled Claude jobs use the raw Claude TUI by default. Use `--ui print` for headless `claude -p` runs.
