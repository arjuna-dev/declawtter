# declawtter

General-purpose assistant workspace for Codex-style memory, session continuity, and reusable operating context.

This repository is no longer a scheduling system. Local job scheduling, cron-style automation, and `launchd` integration were removed from the repo. If you want timed or recurring runs, handle them through Codex-native scheduling outside this workspace.

## What This Repo Is For

- storing durable workspace identity and user context
- keeping distilled daily memory in `MEMORY/`
- keeping reusable workspace guidance in `WORKSPACE/`
- preserving assistant operating rules in `AGENTS.md`
- supporting session continuity without baking runtime state into source code

## What This Repo Does Not Do

- no scheduler CLI
- no `launchd` job management
- no recurring or one-off job installation
- no repo-owned prompt launcher for scheduled Codex runs

## Workspace Structure

- [AGENTS.md](/Users/alejandrocamus/Documents/dev/declawtter/AGENTS.md): workspace bootstrap and handoff contract
- [WORKSPACE](/Users/alejandrocamus/Documents/dev/declawtter/WORKSPACE): identity, user, soul, tools, and skills context
- [MEMORY](/Users/alejandrocamus/Documents/dev/declawtter/MEMORY): distilled day-level memory summaries
- [templates](/Users/alejandrocamus/Documents/dev/declawtter/templates): starter docs for bootstrapping or copying the workspace pattern
- [legacy](/Users/alejandrocamus/Documents/dev/declawtter/legacy): historical notes kept only for reference

## Operating Model

The repo is intentionally lightweight.

The assistant should use files for continuity instead of hidden state:

- write durable facts to `WORKSPACE/`
- write day summaries to `MEMORY/`
- keep reusable workflows documented instead of re-discovering them

If future scheduling is needed, use Codex automation outside this repository rather than reintroducing local scheduling scripts here.
