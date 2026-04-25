# Declaw Workspace

This is a declaw assistant workspace.

## Structure

- `AGENTS.md`: active instructions for Codex in this workspace
- `WORKSPACE/`: reusable skills context
- `MEMORY/`: distilled day-level memory summaries
- `SESSIONS/`: raw session history
- `LOGS/`: local logs and operational output

## Scheduling

Use the global `declaw` CLI for scheduling:

```sh
declaw schedule codex <job> --project <project> --daily 09:00 --prompt "Review this workspace."
declaw schedule claude <job> --project <project> --daily 09:00 --prompt "Review this workspace."
```

Recurring agent schedules require a declaw project. One-off agent schedules may use `--workspace` or no target.
