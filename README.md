# declaw

`declaw` is a reusable assistant workspace template plus a native Go CLI for:

- creating tracked copies of the workspace without Git metadata
- listing and removing tracked copies
- printing project paths for shell `cd` flows
- scheduling Codex and Claude runs through macOS `launchd`

## Commands

```sh
declaw
declaw create my-workspace
declaw list
declaw path my-workspace
cd "$(declaw path my-workspace)"
declaw checkout my-workspace
declaw settings provider claude
declaw ai-agent "Create a recurring Codex schedule for my PM review."

declaw schedule codex morning-review --project my-workspace --daily 09:30 --prompt "Review the repo and summarize blockers."
declaw schedule claude morning-review --project my-workspace --daily 09:30 --prompt "Review the repo and summarize blockers."
declaw schedule list
```

Scheduled Codex jobs use declaw's app-server chat UI by default. Pass `--ui declaw` for the legacy `codex exec` chat UI, or `--ui codex` when you want the raw Codex TUI instead.
Scheduled Claude jobs use the raw Claude TUI by default. Pass `--ui declaw` for declaw's chat UI, or `--ui print` for headless `claude -p` runs. Claude schedules run with `--dangerously-skip-permissions`, so only point them at directories you trust.

For agent-created recurring jobs, always choose or register the project first:

```sh
declaw list
declaw create pm-workspace --into ~/Documents/dev
declaw track product-repo --path ~/Documents/dev/my-codebase
declaw schedule codex pm-deadline-review --project pm-workspace --weekdays 09:00 --prompt "Use the configured PM CLI to update this workspace, inspect the relevant codebase directory named in the workspace docs, and propose deadline follow-ups."
declaw schedule codex repo-risk-review --project product-repo --daily 10:00 --prompt "Inspect this codebase and summarize product-management risks, deadline pressure, and next actions."
declaw schedule claude repo-risk-review --project product-repo --daily 10:00 --prompt "Inspect this codebase with Claude and summarize product-management risks, deadline pressure, and next actions."
```

For one-off agent jobs, `--project` or `--workspace` is optional. If omitted, declaw uses `~/.local/share/declaw/workspaces/one-off` as a scratch workspace:

```sh
declaw schedule codex quick-note --at "2026-04-14 15:30" --prompt "Open an interactive Codex session for this one-off task."
declaw schedule claude quick-note --at "2026-04-14 15:30" --prompt "Open an interactive Claude session for this one-off task."
```

Running `declaw` without arguments opens an interactive command launcher:

- command input at the top
- matching commands listed below
- tracked projects appear as `checkout <project>`, `path <project>`, and `remove <project>` entries
- installed schedules appear as `schedule status <job>`, `schedule run <job>`, and related job-specific actions
- blue default selection
- up and down arrows to move
- Enter to select or run

Free-text launcher input, `declaw checkout`, and `declaw ai-agent` use Codex by default. Switch the default interactive provider with `declaw settings provider claude`, or switch back with `declaw settings provider codex`.

## Generated Workspace Structure

- `AGENTS.md`: bootstrap contract first, then the single source of identity, user, soul, and tools instructions after handoff
- `after_bootsrap_AGENTS.md`: normal workspace instructions template that bootstrap fills in, then renames to `AGENTS.md`
- `WORKSPACE/`: reusable skills context
- `MEMORY/`: distilled day-level memory summaries
- `SESSIONS/`: raw session history
- `PROJECT_DOCUMENTS/`: durable project artifacts

## Notes

- Scheduled agent runs store the effective prompt with workspace bootstrap instructions.
- `declaw create` copies the embedded workspace template by default. Use `--source <dir>` only as an explicit development override.
- `declaw checkout <project>` opens the configured interactive provider in that project's directory. A CLI cannot change the parent shell's directory, so use `cd "$(declaw path <project>)"` when you only need shell navigation.
- `declaw ai-agent [prompt]` opens the configured provider in an embedded declaw-management workspace with instructions for managing projects and schedules through the CLI.
- Recurring agent runs require an explicit declaw project and run with that project directory as context.
- One-off agent runs may omit a directory and use declaw's default one-off workspace.
- Recurring schedules install an optional paired recovery job by default.
- This implementation is currently macOS-focused because scheduling uses `launchd`.
