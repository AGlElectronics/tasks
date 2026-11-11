# ADR 0006: Live updates with Server-Sent Events from PostgreSQL NOTIFY

- Status: accepted (November 2026)

## Context

Boards polled every 15 seconds. Several people (and automations) work the
same lists during an event; changes should show within a second, without
a WebSocket stack and without leaking content to people who may not see
it.

## Decision

- **Source:** database triggers on tasks and everything hanging off them
  (comments, checklists, assignees, tags, watchers, attachments, field
  values, dependencies, time) announce `{"k":"list","l":…,"s":…}` on the
  channel `tasks_changes`; structure and sharing tables (spaces, lists,
  statuses, folders, tags, grants, automations, custom fields) announce
  `{"k":"space","s":…}`; notifications announce `{"k":"inbox","u":…}`.
  Triggers catch every writer: handlers, automations, background jobs and
  manual SQL. PostgreSQL drops identical payloads within a transaction, so
  a bulk change sends one message per list.
- **Fan-out:** one `LISTEN` connection per backend (`internal/realtime`)
  hands each change to the open streams.
- **Delivery:** `GET /api/v1/stream` (Server-Sent Events, signed in). Each
  stream knows which lists and spaces its person can see (recomputed on
  space changes, inbox events and every two minutes) and forwards only
  those. Events say *what* changed, never the content; the browser
  refetches through the normal, access-checked API.
- **Browser:** `EventSource` cannot send the bearer token, so the web app
  reads the stream with `fetch`. While connected it polls every two
  minutes instead of 15 seconds; on errors it reconnects with back-off and
  falls back to 15-second polling.
- **Limits:** 5 streams per person; a 25-second heartbeat (below nginx's
  60-second idle timeout); the server closes a stream after 30 minutes so
  the browser reconnects with a fresh token. A slow stream that misses
  changes gets a `resync` event (refetch everything).

## Consequences

- Works across backend replicas without changes ([ADR 0001](0001-single-backend-replica.md)).
- Proxies must not buffer `text/event-stream` (the backend sends
  `X-Accel-Buffering: no`) and must not cut requests after a short timeout:
  the Helm chart routes `/api/v1/stream` like a websocket path.
- Every write costs a trigger call and a `pg_notify`; at Tasks' volume this
  is negligible.
