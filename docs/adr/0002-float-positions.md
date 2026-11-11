# ADR 0002: Float positions with automatic renumbering

- Status: accepted (November 2026)

## Context

Dragging a card on a board or a row in a list changes the manual order.
The plan proposed fractional-index strings (`a0`, `a0V`), which never need
renumbering but are hard to read in the database and need a library.

## Decision

`tasks.position` is a `DOUBLE PRECISION`. New tasks go to the end
(`max + 1024`); a drop takes the midpoint of its new neighbours
(`frontend/src/lib/board.ts`, `dropPosition`). When a gap between two
neighbours falls below `1e-9`, the backend renumbers the whole list to
steps of 1024 in the same transaction as the move. Renumbering bumps no
versions: the relative order does not change, so concurrent edits stay
valid.

## Consequences

- One row changes per drop in the normal case.
- About 40 drops into the same gap trigger a renumber of one list; that is
  rare and cheap.
- Positions are plain numbers anyone can read in SQL.
