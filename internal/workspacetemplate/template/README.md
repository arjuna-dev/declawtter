# Declaw Workspace

This is a declaw assistant workspace.

## Structure

- `AGENTS.md`: active instructions for Codex in this workspace
- `WORKSPACE/`: identity, user, soul, tools, and reusable skills context
- `MEMORY/`: distilled day-level memory summaries
- `SESSIONS/`: raw session history
- `PROJECT_DOCUMENTS/`: durable project artifacts
- `LOGS/`: local logs and operational output

## Scheduling

Use the global `declaw` CLI for scheduling:

```sh
declaw schedule codex <job> --project <project> --daily 09:00 --prompt "Review this workspace."
```

Recurring Codex schedules require a declaw project. One-off Codex schedules may use `--workspace` or no target.
