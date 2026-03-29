# declawtter

General-purpose assistant workspace for openClaw-style workspace, memory, session continuity, and reusable operating context.

## What This Repo Is For

- give a nice personality for agents based on the openClaw workspace
- storing durable workspace identity and user context in file system
- keeping reusable workspace guidance in `WORKSPACE/`
- preserving assistant operating rules in `AGENTS.md`
- supporting session continuity without baking runtime state into source code

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
