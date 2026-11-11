# ADR 0001: One backend replica for 1.x

- Status: accepted (November 2026)
- Deciders: maintainer

## Context

Tasks' backend keeps some state in the process and runs timed jobs in it:

| What | Where | What breaks with two replicas |
| --- | --- | --- |
| Per-person rate limit (20 requests/s, burst 100) | memory | each replica allows the full rate, so the real limit doubles |
| Live-update stream limit (5 per person) | memory | each replica allows 5 |
| Due-date automations, every minute | timer | two replicas pick the same task; the run history deduplicates, but both can run the rule before either records it |
| Due-soon reminders, every 15 minutes | timer | the same reminder can be written twice before `due_reminders` records it |
| Email outbox, every minute | timer | two replicas can email the same notification |
| Retention, every hour | timer | harmless (deletes are idempotent), but double work |

Live updates themselves already work across replicas: changes are
announced through PostgreSQL `LISTEN/NOTIFY` ([ADR 0006](0006-live-updates-sse.md)),
so every replica sees every change.

The expected load (one organisation, hundreds of people, one event at a
time) is far below what one Go process with a connection pool handles.

## Decision

Run exactly **one backend replica** in 1.x. The Helm chart refuses any
other value (`backend.replicas` must be 1, `backend.singleReplica: true`)
and gives the backend no PodDisruptionBudget, so node drains are not
blocked. The web app (`tasks-frontend`) is stateless and runs two or more
replicas.

A restart or node failure makes the API unavailable for the seconds the
new pod needs to start; the web app retries and the live-update stream
reconnects on its own.

## When to revisit, and what scaling out needs

Revisit when one replica is not enough (CPU, connection count of open
streams) or when an outage of a few seconds per deploy is no longer
acceptable. Scaling out then needs, in one change:

1. Jobs under a PostgreSQL advisory lock (`pg_try_advisory_lock` per job),
   so one replica runs each tick; or `FOR UPDATE SKIP LOCKED` on the rows
   each job takes.
2. The rate limit and the stream limit in PostgreSQL (or in a shared store),
   or accept per-replica limits and divide the numbers by the replica count.
3. The chart: drop `singleReplica`, allow `replicas > 1`, add the
   PodDisruptionBudget and a `RollingUpdate` strategy.

Nothing else (sessions, uploads, the stream) holds state in the process.

## Consequences

- Deploys and node failures cause a short API outage.
- Background jobs need no locking code today.
- The live-update design does not have to change to scale out.
