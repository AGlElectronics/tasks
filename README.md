# Tasks

> [!WARNING]
> ## **NOT READY FOR PRODUCTION**
>
> Con Services is under active development. Expect breaking changes, missing
> features and bugs. Do not use it for real events or real data yet.

Project and task tracking for the Con Services stack: ClickUp-style
spaces with lists and boards, built for convention crews that hated
Vikunja's clutter. The plan and the decisions behind it are in
[docs/plan.md](docs/plan.md).

Built: spaces with per-space statuses (board columns) and `KEY-N` task
IDs, lists and board views with drag and drop, subtasks, My Work grouped
by due date, and deliberately minimal task creation — a title is enough,
everything else is optional or hidden behind "More fields". On top of
that:

- **Automations:** "when X happens, do Y" rules per space or per list —
  six triggers (created, status changed, assignee added, comment added,
  checklist completed, due approaching), conditions on list/status/
  priority/tag/assignee, ten actions (set status/priority/due, assign,
  tag, move, subtask, checklist item, comment, notify). Rules run in the
  same transaction as the change, chains are loop-guarded (each rule at
  most once per change, depth ≤ 5), every run lands in a visible history.
- **Collaboration:** comments with `@Full Name` mentions and editing,
  watchers, and an in-app Inbox with an unread badge; assignment and
  status changes notify followers.
- **Organisation:** tags per space, checklists on tasks, folders grouping
  lists in the sidebar (drag a list onto a folder), favourites, Ctrl+K
  search with task-ID jump, filters and bulk actions in list view.
- **Attachments:** files on tasks (25 MB), stored in S3 and streamed
  through the backend behind the access check. Optional: without
  `S3_ENDPOINT` uploads answer 503 and everything else works.
- **Safety:** optimistic locking (version + 409), soft-delete trash with
  restore/purge, list archiving, an audit log for destructive actions,
  and `KEY-N` aliases so references keep resolving after a space key
  rename or a cross-space move.
- **Planning:** custom fields per space (text, number, date, select,
  checkbox, URL), dependencies ("waits for"), recurring tasks, time
  tracking with estimates, and calendar, timeline and workload views (per
  list, plus a calendar across every space).
- **Live updates:** boards and task pages refresh within a second over
  Server-Sent Events fed by PostgreSQL `LISTEN/NOTIFY`; polling is the
  fallback.
- **Email:** each person chooses which notifications also come by email;
  due-soon reminders; SMTP from Den or `SMTP_*`.
- **Admin:** audit log page and API, retention and reminder settings
  (from Den's module settings when paired), import from CSV, ClickUp and
  Vikunja.
- **Scale:** cursor pagination (`X-Next-Cursor`), per-user rate limiting,
  position rebalancing when drag-and-drop exhausts float gaps.
- **Departments:** HR calls `/api/internal/departments` (guarded by
  `TASKS_SERVICE_TOKEN`, off when empty) whenever a department is created,
  saved or archived; each department gets exactly one space, closed by
  default, with the department's Keycloak group granted edit.
  `scripts/backfill-department-spaces.ps1` seeds existing departments.

Decisions with lasting consequences are in [docs/adr/](docs/adr/).

- **Backend:** Go 1.27, chi, pgx (hand-written queries), zerolog.
- **Frontend:** React 19 + Rsbuild + TanStack Router/Query + Tailwind.
- **Database:** PostgreSQL 16, migrated via Goose (`cmd/migrate` one-shot).
- **Object storage:** S3 (`S3_*` env, bucket `tasks-data-dev` in dev),
  optional; include the bucket in backups when attachments are used.

## Reserved ports

| Service        | Host port | Container port |
| -------------- | --------- | -------------- |
| backend (API)  | **8160**  | 8080           |
| frontend (SPA) | **3009**  | 80             |
| dev-copy backend  | **8161** | 8080          |
| dev-copy frontend | **3010** | 80            |

The `tasks-dev` compose profile is a parallel instance on the `tasks_dev`
database for developing against while the live one keeps serving.

## Access model

Module roles come from IdP groups via `infra/access-config.toml`, checked
on every request:

| Keycloak group  | Role                 | May do                                          |
| --------------- | -------------------- | ----------------------------------------------- |
| `tasks-admin`   | `organization_admin` | Manage every space; brand/settings; Den pairing. |
| `tasks-creator` | `space_creator`      | Create spaces (and manage the ones they create). |

**Reading and editing is not a role.** It comes from grants inside Tasks,
at one of three levels: view, edit, manage (manage = share, rename,
statuses, automations, trash, delete).

- **Spaces start closed.** Share them with people, Keycloak groups, or
  everyone in the organisation at a chosen level.
- **Single lists can be shared** so another team works in one list
  without seeing the rest of the space; **private lists** are visible
  only to people with explicit grants on them.
- **Assignment opens one task at edit level; watching opens it at view
  level** — so "please handle this one ticket" needs no space access.
- Spaces and tasks you cannot see answer 404, not 403.
- Automations: space managers run the space's rules; a manager of one
  list can manage rules scoped to that list.

Group grants match the `groups` claim on the token (case-insensitive, one
leading `/` ignored, whole path), so removing someone from a Keycloak
group revokes that access on their next request.

## Run it

In the suite (from the repo root):

```powershell
docker compose --profile tasks up --build -d
```

Then https://tasks.conservices.test (or http://localhost:3009), signed in
through Keycloak. The dev copy: `--profile tasks-dev`, then
http://localhost:3010.

## Tests

```powershell
cd tasks/backend
$env:TEST_DATABASE_URL = 'postgres://tasks:tasks@localhost:5432/tasks_test?sslmode=disable'
go test ./...
```

The tests drop and recreate the `public` schema of that database, so
point them at a throwaway one. They cover the authorization matrix
(roles, grants, group matching, private lists, assignment and watching,
the trash), automations, patch semantics and version conflicts, status
replacement, `KEY-N` resolution, the HR department hook, the audit log,
settings, retention, reminders and email, live updates, custom fields,
dependencies, recurrence, time tracking, the calendar and import. Frontend unit tests run
with `npm test` in `tasks/frontend`. Against the running stack,
`scripts/smoke-tasks.ps1` walks the whole feature surface in 15 steps
(`-Base http://localhost:8161` to point it at the dev copy).

## Operations notes

- **Run one backend replica** ([ADR 0001](docs/adr/0001-single-backend-replica.md)):
  the rate limiter and the background jobs (due automations, reminders,
  email, retention) run in the process.
- Proxies must not buffer or time out `/api/v1/stream` (live updates).
- Back up Postgres, and the `tasks-data-dev` bucket when attachments are
  in use.
- `TASKS_SERVICE_TOKEN` guards `/api/internal/departments` (HR's hook);
  leave it empty to turn the internal API off. HR side: add
  `url|token` to `DEPARTMENT_HOOKS`.
- Keycloak client `tasks-spa` needs the SPA origin in its redirect URIs
  (the dev copy uses http://localhost:3010/*).
