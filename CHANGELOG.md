# Changelog

All notable changes to Tasks are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

Needs Den: 1.0.0 or later (Den MAJOR 1).

First release, `1.0.0`. Released versions are tagged `vMAJOR.MINOR.PATCH`;
the images `ghcr.io/aglelectronics/tasks-backend`, `tasks-frontend` and
`tasks-migrate` are published as `MAJOR.MINOR.PATCH-BUILD`, where `BUILD`
counts rebuilds of the same version. Rebuilds get no entry here.

### Added

- Spaces with lists and boards. A task needs only a title; due dates,
  priorities, checklists and tags are optional.
- Each HR department gets its own space automatically, shared with the
  department's group, through the department hook HR calls
  (`POST /api/internal/departments`, HR's `DEPARTMENT_HOOKS`) when a
  department is created or saved. Renames follow HR; `"archived": true`
  archives the space and a later save without it restores it. Unknown
  fields in HR's request are ignored, so HR can add fields first.
- Spaces can be archived (by a space manager or HR's department hook): they
  leave the sidebar and search, keep every task, and are listed under
  *Show archived spaces* (`GET /api/v1/spaces?archived=1`).
- My Work lists each person's tasks by due date, and the Inbox tells them
  when they are assigned, mentioned, or a task they follow moves.
- Tasks move between lists and spaces, a list can be shared with another
  team, and task keys such as `CREW-42` keep working after a move.
- Automation rules (for example "when a task is done, move it to the
  archive list"), version checks against overwriting each other, a trash
  for deleted tasks, and an audit log of destructive actions.
- `DEN_MODE=required` keeps the module locked until it is paired with Den:
  the API answers `503 den_pairing_required` (except health, brand, sign-in
  and the Den connection routes) and the public `GET /api/den/state`
  (also at `/api/v1/den/state`) reports `{"mode","paired"}`. The default
  `standalone` runs without Den; any other value stops the backend at
  startup.
- The web app shows a "Connect this module to Den" page (admins) or
  "Waiting for Den" (everyone else) while the backend runs with
  `DEN_MODE=required` and is not paired yet.
- `TRUSTED_PROXY_CIDRS` (optional): comma-separated CIDRs of the proxies in
  front of the backend whose `X-Forwarded-For` entries are believed. The
  default, `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,::1/128,fc00::/7`,
  covers the Gateway, nginx and Caddy; change it only for a proxy outside
  the private ranges, or set `none` when no proxy is in front. See
  [Client IP addresses](https://aglelectronics.github.io/con-services/operations/production/#client-ip-addresses).
- The backend can serve HTTPS. Set `TLS_CERT_FILE` and `TLS_KEY_FILE` to a
  PEM certificate and private key; the backend re-reads both every minute,
  so a certificate renewed by cert-manager is used without a restart. With
  neither set it serves plain HTTP, which is what Docker Compose
  development uses. See
  [Internal TLS](https://aglelectronics.github.io/con-services/operations/internal-tls/).
- The web app reads its installation settings (sign-in mode, issuer, client
  id, scopes, API address and product name) from `/config.json` when it
  starts, so the published `tasks-frontend` image works for every
  installation. nginx serves the file from `/etc/conservices/config.json`
  with `Cache-Control: no-store`, so a change shows on the next page load.
  Without a mounted file the web app shows "Not configured yet"; a broken
  file gives a clear error page. See
  [Web app settings](https://aglelectronics.github.io/con-services/modules/tasks/configuration/#web-app-settings-configjson).
- Kubernetes: Helm chart `deploy/charts/tasks` `1.0.0` in `con-services`,
  released with the tag `tasks/v1.0.0`. Internal TLS is on by default:
  cert-manager issues `tasks-backend-tls` and `tasks-frontend-tls`, the
  backend and the web app serve HTTPS inside the cluster (web app Service
  on port 443, backend on 8080, both named `https`), and every in-cluster
  address uses `https://`. The cluster needs cert-manager, trust-manager
  and the platform's `conservices-internal-ca` issuer; without them set
  `internalTLS.enabled: false`. See
  [Set up Tasks](https://aglelectronics.github.io/con-services/modules/tasks/setup/).
- Development: `infra/docker-compose.yml` mounts
  `infra/frontend-config/compose.json` (dev sign-in) as the web app's
  `/config.json`; set `FRONTEND_CONFIG` in `.env` to use another file.
  Building from source needs Go 1.27.3 or newer.
- Calendar, timeline and workload views. A list can be shown as a month
  calendar, a timeline (a bar from start to due date per task) or a
  workload table (open tasks and estimated time per person per week, plus
  the time logged). *Calendar* in the sidebar shows every dated task you
  can see across spaces (`GET /api/v1/calendar?from=&to=[&mine=1]`, at most
  120 days and 2,000 tasks).
- The list view sorts the way you choose (priority then due date, manual
  board order, due date, priority, title, newest, recently updated),
  filters by a select custom field, and remembers its filters and sort per
  list in your browser.
- Custom fields: a space's managers define fields of type text, number,
  date, select, checkbox or URL (*Space → Custom fields*,
  `GET`/`POST /api/v1/spaces/{space}/fields`, `PATCH`/`DELETE
  /api/v1/fields/{field}`); anyone who can edit a task sets the values
  (`PATCH /api/v1/tasks/{task}` with `"fields": {"<field id>": value}`,
  `null` clears). Tasks carry `fields` in every response, changes appear in
  the activity log, values stay behind when a task moves to another space,
  and deleting a field (with all its values) is audited.
- Dependencies: a task can wait for others (`POST
  /api/v1/tasks/{task}/dependencies` with `{"depends_on": "KEY-N"}`,
  `DELETE …/dependencies/{other}`), also across spaces. Circles are
  refused. Tasks show `blocked_by_open`; the task page lists what it waits
  for and what waits for it, leaving out tasks you cannot see
  (`hidden_dependencies`).
- Recurring tasks: `"recurrence": {"freq": "daily"|"weekly"|"monthly"|"yearly",
  "interval": n}` on a top-level task. Completing it (by hand or by an
  automation) creates the next occurrence with start and due dates moved
  on, the same list, assignees, watchers, tags, custom field values and an
  unchecked checklist; the rule moves to the new task.
- Time tracking: log time on a task (`POST /api/v1/tasks/{task}/time` with
  `minutes`, optional `spent_on` and `note`; `GET …/time`; `DELETE
  /api/v1/time/{entry}` by the person who logged it or a space manager),
  set an estimate (`estimate_minutes` on `PATCH /api/v1/tasks/{task}`), and
  see totals per person for a list (`GET
  /api/v1/lists/{list}/time-report?from=&to=`). Tasks carry
  `estimate_minutes` and `time_spent_minutes`.
- Live updates: boards, lists, task pages, the sidebar and the Inbox
  refresh within a second when someone else changes something, over
  Server-Sent Events at `GET /api/v1/stream` (signed in; at most 5 streams
  per person). Database triggers announce *which* list, space or inbox
  changed on the PostgreSQL channel `tasks_changes`; each person only hears
  about lists and spaces they can see, and the browser refetches through
  the normal API. While connected, background polling slows from 15
  seconds to 2 minutes; without the stream it stays at 15 seconds. Proxies
  must not buffer the stream (the backend sends `X-Accel-Buffering: no`)
  and must allow idle reads of at least 30 seconds (heartbeat every 25).
- Email notifications. Each person picks which Inbox notifications also
  come by email (*Inbox → Email settings*, `GET`/`PUT
  /api/v1/me/notifications`; default: assigned, mentioned, due soon).
  Emails wait two minutes, bundle what happened meanwhile and skip what
  was already read in the app. SMTP comes from Den when the module is
  paired and Den sends it, otherwise from the new `SMTP_HOST`,
  `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`,
  `SMTP_FROM_NAME` and `SMTP_SECURITY` (`starttls`, `tls` or `none`);
  `TASKS_PUBLIC_URL` puts links in the emails. Admins send a test email
  from *Settings* (`POST /api/v1/settings/email/test`).
- Due-soon reminders: assignees get an Inbox notification (and email, if
  chosen) when an open task is due within `reminders.due_soon_days`
  (default 1 day, and on the day), once per due date.
- Retention: deleted tasks are purged from the trash after
  `retention.trash_days` (30), read notifications after
  `retention.notification_days` (90), audit entries after
  `retention.audit_days` (730) and automation runs after
  `retention.automation_run_days` (90); `0` keeps forever. The defaults are
  placeholders pending legal review. Admins change them on *Settings*
  (`GET`/`PUT /api/v1/settings`); when paired, values Den sends in its
  module settings win and are read-only in Tasks. Purges by retention are
  audited with the actor *System*, and purging a task (by hand or by
  retention) now also deletes its attachment files from S3.
- Admins read the audit log in the web app (*Settings → Audit log*) or at
  `GET /api/v1/audit` (`settings.manage`): deletes, purges, grants given
  and removed, archived and restored spaces and settings changes, newest
  first, filtered by `entity`, `action`, `actor`, `entity_id`, `since`
  and `until` and paged with `X-Next-Cursor`. Entries made by the system
  (retention sweep, HR's department hook) show the actor *System*.
- Long task histories stay fast: `GET /api/v1/tasks/{task}` embeds the
  newest 100 comments and 200 activity entries plus `comments_total` and
  `events_total`; `GET /api/v1/tasks/{task}/comments` and `…/events` page
  through the rest (`?limit=`, `X-Next-Cursor`). The task page offers
  *Show N older entries*. The sidebar (`GET /api/v1/spaces`) returns at
  most 1,000 spaces and says `"truncated": true` when there are more.
- CI on every pull request (tests, lint, Trivy scans) and signed images with
  an SBOM published to GHCR on version tags (linux/amd64; linux/arm64
  optional).

### Security

- Group grants match IdP groups the way every Con Services module will:
  spaces trimmed, one leading `/` ignored (Keycloak's full-path form),
  case-insensitive, and the whole path must match (`/departments/ops`
  matches `departments/ops` but not `ops`). Grants show the name as it was
  typed (`subject`) and match on `subject_key`. On upgrade, the migration
  fills `subject_key` for existing grants and merges grants that only
  differed in case or a leading `/`, keeping the strongest level.
- Giving and removing access (grants) is written to the audit log.

- Rate limits cannot be bypassed with a fake `X-Forwarded-For` header: the
  backend reads the client address from it only when the connection comes
  from a trusted proxy (`TRUSTED_PROXY_CIDRS`), and never reads
  `X-Real-IP` or `True-Client-IP`.
- On Kubernetes the database URL Den shows ends in
  `?sslmode=verify-full&sslrootcert=/etc/conservices-internal-ca/ca.crt`;
  the platform's PostgreSQL refuses connections without TLS.
- The web app's Content-Security-Policy limits scripts, frames and other
  content to the web app itself and allows HTTPS connections to any host
  (`connect-src 'self' https:`), so sign-in works with the Keycloak address
  set in `config.json`.
- No image runs as root. The web app runs on
  `nginxinc/nginx-unprivileged:1.30.6-alpine` as UID 101 and listens on
  port 8080. All images are built with Go 1.27.3 and pass the CI image
  scan (Trivy, HIGH and CRITICAL issues that have a fix).
- A production build never falls back to dev sign-in: a `/config.json`
  without `authMode` means OIDC.
- The backend fails closed: without `APP_ENV` and `AUTH_MODE` it starts as
  `APP_ENV=production` with `AUTH_MODE=oidc` and refuses to start until
  `OIDC_ISSUER_URL` and `OIDC_CLIENT_ID` are set. Development setups set
  `APP_ENV=development` and `AUTH_MODE=dev` explicitly (Docker Compose and
  `.env.example` already do).
