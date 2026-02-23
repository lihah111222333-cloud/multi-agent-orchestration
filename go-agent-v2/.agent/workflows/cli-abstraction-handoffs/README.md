# cli-abstraction handoffs

This directory is the canonical handoff location for the `cli-abstraction.md` workflow.

## Required files per phase

For phase `N` in `1..5`, write:

1. `pN.md` - phase report and handoff summary.
2. `pN.checks.log` - command outputs and validation logs (append-only).
3. `pN.files.txt` - changed file list for this phase.

When blocked, also write:

1. `pN.blockers.md` - blocker details, impact, and next action.

## Global pointer

Always update `LATEST.md` when phase state changes.

Fields required in `LATEST.md`:

1. `current_phase`
2. `status`
3. `next_phase`
4. `updated_at`
5. `owner`

