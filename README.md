# declaw

`declaw` is a reusable assistant workspace template plus a native Go CLI for:

- creating tracked copies of the workspace without Git metadata
- listing and removing tracked copies
- printing project paths for shell `cd` flows
- scheduling Codex runs and simple reminders through macOS `launchd`

## Commands

```sh
declaw
declaw create my-workspace
declaw list
declaw path my-workspace
cd "$(declaw path my-workspace)"

declaw schedule codex morning-review --project my-workspace --daily 09:30 --prompt "Review the repo and summarize blockers."
declaw schedule reminder drink-water --daily 14:00 --title "Reminder" --body "Drink water."
declaw schedule list
```

For agent-created recurring Codex jobs, always choose or register the project first:

```sh
declaw list
declaw create pm-workspace --into ~/Documents/dev
declaw track product-repo --path ~/Documents/dev/my-codebase
declaw schedule codex pm-deadline-review --project pm-workspace --weekdays 09:00 --prompt "Use the configured PM CLI to update this workspace, inspect the relevant codebase directory named in the workspace docs, and propose deadline/reminder follow-ups."
declaw schedule codex repo-risk-review --project product-repo --daily 10:00 --prompt "Inspect this codebase and summarize product-management risks, deadline pressure, and reminder candidates."
```

For one-off Codex jobs, `--project` or `--workspace` is optional. If omitted, declaw uses `~/.local/share/declaw/workspaces/one-off` as a scratch workspace:

```sh
declaw schedule codex quick-note --at "2026-04-14 15:30" --prompt "Open an interactive Codex session for this one-off task."
```

Running `declaw` without arguments opens an interactive command launcher:

- command input at the top
- matching commands listed below
- blue default selection
- up and down arrows to move
- Enter to select or run

## Workspace Structure

- [draft_AGENTS_doNotUse.md](/Users/alejandrocamus/Documents/dev/declawtter/draft_AGENTS_doNotUse.md): bootstrap contract template for generated workspaces
- [WORKSPACE](/Users/alejandrocamus/Documents/dev/declawtter/WORKSPACE): identity, user, soul, tools, and skills context
- [MEMORY](/Users/alejandrocamus/Documents/dev/declawtter/MEMORY): distilled day-level memory summaries
- [SESSIONS](/Users/alejandrocamus/Documents/dev/declawtter/SESSIONS): raw session history
- [PROJECT_DOCUMENTS](/Users/alejandrocamus/Documents/dev/declawtter/PROJECT_DOCUMENTS): durable project artifacts

## Notes

- Scheduled Codex runs store the effective prompt with workspace bootstrap instructions.
- Recurring Codex runs require an explicit declaw project and run Codex with that project directory as context.
- One-off Codex runs may omit a directory and use declaw's default one-off workspace.
- Recurring Codex schedules install an optional paired recovery job by default.
- This implementation is currently macOS-focused because scheduling uses `launchd`.
