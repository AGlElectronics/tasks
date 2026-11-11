# ADR 0003: Three status categories, statuses per space

- Status: accepted (November 2026)

## Context

Statuses are the board's columns. Each needs a meaning the system
understands (is the task done? does it count as open in My Work?). The
plan had four categories (`not_started`, `active`, `done`, `closed`) and
per-list status sets.

## Decision

Three categories: `not_started`, `active`, `done`. Nobody could say what
`closed` adds over `done`; finished work that should leave the board goes
to an archived list. Statuses belong to the **space**; every list in it
uses the same set. A space must keep at least one open and one done
status. Removing a status moves its tasks to the first status of the same
category (or the first status), with an activity entry per task.

## Consequences

- Boards across lists of a space, automations, imports and custom fields
  all see one status set.
- Moving a task to another space maps its status by category.
- A department that wants a different workflow for one list makes a second
  space.
