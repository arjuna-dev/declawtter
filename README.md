# declaw

`declaw` is a native Go CLI for:

- creating or tracking empty project directories
- opening projects with Pi, Hermes, Codex, or Claude
- discovering recent activity from local agent state
- scheduling supported harnesses through macOS `launchd`

## Commands

```sh
declaw
declaw create my-workspace --into ~/Documents/dev --harness codex
declaw track existing-project --path ~/Documents/dev/my-repo --harness claude
declaw list
declaw project-settings existing-project give alias "Customer Portal"
declaw project-settings existing-project harness claude
declaw project-settings existing-project pin on
declaw project-settings existing-project ignore on
declaw project-settings existing-project path
declaw checkout
declaw checkout existing-project continue
declaw checkout existing-project change-harness pi
declaw settings harness claude
declaw settings terminal ghostty
declaw schedule create morning-review --provider default --project existing-project --daily 09:30 --prompt "Review the repo and summarize blockers."
declaw schedule list
```

The supported harnesses are `pi`, `hermes`, `codex`, and `claude`.

`declaw create` creates or registers the project directory only. It does not copy a workspace template or create memory, session, or other project files.

For agent-created recurring jobs, always choose or register the project first:

```sh
declaw list
declaw create pm-workspace --into ~/Documents/dev --harness codex
declaw track product-repo --path ~/Documents/dev/my-codebase --harness claude
declaw project-settings product-repo harness claude
declaw schedule create repo-risk-review --provider default --project product-repo --daily 10:00 --prompt "Inspect this codebase and summarize product-management risks, deadline pressure, and next actions."
declaw schedule pi pm-deadline-review --project pm-workspace --weekdays 09:00 --prompt "Review the project and propose deadline follow-ups."
declaw schedule hermes repo-risk-review --project product-repo --daily 10:00 --prompt "Inspect this codebase and summarize product-management risks, deadline pressure, and next actions."
declaw schedule codex repo-risk-review --project product-repo --daily 10:00 --prompt "Inspect this codebase and summarize product-management risks, deadline pressure, and next actions."
declaw schedule claude repo-risk-review --project product-repo --daily 10:00 --prompt "Inspect this codebase with Claude and summarize product-management risks, deadline pressure, and next actions."
```

For one-off agent jobs, `--project` or `--workspace` is optional. If omitted, declaw uses `~/Library/Application Support/declaw/support/workspaces/one-off` as a scratch workspace:

```sh
declaw schedule codex quick-note --at "2026-04-14 15:30" --prompt "Open an interactive Codex session for this one-off task."
declaw schedule claude quick-note --at "2026-04-14 15:30" --prompt "Open an interactive Claude session for this one-off task."
```

Running `declaw` without arguments opens an interactive command launcher:

- command input at the top
- matching commands listed below
- checkout shows known projects ordered by most recent agent activity
- project settings is ordered by pinned status and most recent agent activity; it contains path, remove, harness, alias, pin, and ignore actions
- UUID-like project identifiers are shown using a readable project directory name when no alias has been set
- installed schedules appear as `schedule status <job>`, `schedule run <job>`, and related job-specific actions
- blue default selection
- up and down arrows to move
- Enter to select or run

Free-text launcher input and `declaw ai-agent` use the configured harness. Switch it with `declaw settings harness <pi|hermes|codex|claude>`. Checkout uses the most recently used harness for each project and falls back to the project's configured harness or the global setting. Pinned projects appear first, and ignored projects remain in project settings but are omitted from checkout.

Scheduled interactive jobs open macOS Terminal by default. Use `declaw settings terminal ghostty` to open scheduled terminal jobs in Ghostty, or `declaw settings terminal terminal` to switch back.

## Testing

Use two layers of tests:

- `go test ./...`
  Fast regression coverage for local CLI behavior. This suite exercises the command surface through `internal/app` with fake `codex` and `claude` binaries so create/track/list/path/remove/settings/project/schedule/help/checkout/ai-agent routing regressions fail quickly.
- `DECLAW_RUN_LIVE_E2E=1 go test ./... -run TestLiveAIAgentResponseE2E`
  Opt-in live provider coverage. This builds the real `declaw` binary, points it at a temporary HOME, and verifies that `declaw ai-agent` gets a real model response.
- `DECLAW_RUN_LIVE_SCHEDULE_E2E=1 go test ./... -run TestLiveClaudeSchedulePrintE2E`
  Opt-in macOS launchd coverage. This schedules a one-off Claude `--ui print` job a couple of minutes in the future, waits for execution, and asserts that the scheduled run log contains the expected model output.

The live schedule test is intentionally separate from the default suite because it depends on macOS `launchd`, a working Claude CLI, and a few minutes of wall-clock time.

## Activity discovery

Activity adapters read local agent state without changing it. They cover Codex CLI and desktop metadata, Claude Code and desktop or Cowork metadata, Hermes CLI and desktop state, and Pi sessions or working-directory metadata when a reliable path is present. Projects can be discovered without manual registration.

Known locations include standard home, XDG, and macOS Application Support directories. Custom agent homes and config locations are honored, as are `DECLAW_<HARNESS>_ACTIVITY_PATHS` and `DECLAW_ACTIVITY_PATHS`. SQLite databases are opened read-only, and only declaw's own registry is updated.

## Notes

- The tracked-project registry lives under `~/Library/Application Support/declaw/projects.json` on macOS.
- The embedded `declaw ai-agent` management workspace is materialized under `~/Library/Application Support/declaw/ai-agent`.
- Scheduled-run support data lives under `~/Library/Application Support/declaw/support`.
- The default one-off workspace lives under `~/Library/Application Support/declaw/support/workspaces/one-off`.
- Scheduled agent runs store the effective prompt with workspace bootstrap instructions.
- `declaw checkout <project>` opens the selected harness in that project's directory.
- `declaw ai-agent [prompt]` opens the configured harness in the separate declaw-management workspace.
- Recurring schedules require an explicit declaw project and run with that project directory as context.
- One-off agent runs may omit a directory and use declaw's default one-off workspace.
- Recurring schedules install an optional paired recovery job by default, and one-off schedules can keep an optional fallback twin installed until completion is marked.
- This implementation is currently macOS-focused because scheduling uses `launchd`.
