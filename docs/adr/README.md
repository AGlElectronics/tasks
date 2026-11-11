# Architecture decision records

Decisions about Tasks with lasting consequences. Each record says what was
decided, why, and what it costs.

| ADR | Decision |
| --- | --- |
| [0001](0001-single-backend-replica.md) | One backend replica for 1.x; what scaling out needs |
| [0002](0002-float-positions.md) | Float positions with automatic renumbering |
| [0003](0003-three-status-categories.md) | Three status categories, statuses per space |
| [0004](0004-plain-text-descriptions.md) | Plain-text descriptions and comments |
| [0005](0005-grants-and-group-matching.md) | Grants, private lists and how group names match |
| [0006](0006-live-updates-sse.md) | Live updates with Server-Sent Events from PostgreSQL NOTIFY |
| [0007](0007-settings-and-smtp-from-den.md) | Module settings and SMTP from Den, with Tasks' own fallback |
