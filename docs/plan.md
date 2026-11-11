# Tasks — plan

Project and task tracking for the Con Services suite, structured like
ClickUp (spaces, folders, lists, tasks with custom statuses, list and board
views) with the feature set closer to Vikunja's: the core of planning and
tracking work without ClickUp's long tail. Same shape as every other module
(own database, own bucket, OIDC against the shared Keycloak, role catalog,
Den pairing, Den's nightly backups).

Status: **built** (November 2026), including every "Later" item from the
backlog (custom fields, calendar, timeline and workload, live updates,
email, dependencies, recurring tasks, time tracking, automations, import).
The sections after "Built" are the original plan, kept for the reasoning;
lines marked *(superseded)* no longer describe the module. Where the built
thing deviates from the plan, the next section and the ADRs in
[`docs/adr/`](adr/) are the truth.

## Built, and how it deviates from the plan

Everything in milestones 1–7 exists: spaces, lists, boards, statuses,
tasks with `KEY-N` IDs, subtasks, assignees, tags, checklists, comments
with mentions and editing, watchers, Inbox, My Work, Ctrl+K search,
favourites, folders, attachments, trash, archiving, audit log, grants,
smoke script and docs. **Automations shipped too** (planned "later"):
six triggers, conditions, ten actions, same-transaction execution, loop
guard, due-date sweep, run history, builder UI with templates — they were
the loudest item in the Vikunja feedback ("no good way to automate").

Deviations from the plan, deliberate:

| Planned | Built | Why |
| --- | --- | --- |
| Fractional-index strings for ordering | Float positions + automatic renumbering when a gap closes | Simpler; the rebalance (TASK-37) runs in the same transaction and bumps no versions. |
| Status categories incl. `closed` | Three categories: `not_started`, `active`, `done` | Nobody could say what `closed` adds over `done`; archive the list instead. |
| BlockNote JSON descriptions and comments | Plain text | The wiki is for documents; task descriptions stayed deliberately light (part of the "too many fields" feedback). |
| Per-list status sets | Statuses per space only | Keeps cross-list boards and automations coherent; nobody asked for per-list sets. |
| Access levels view/comment/edit/full, restriction | view/edit/manage; private lists instead of restriction | Three levels cover the real cases; a private list is the understandable version of "restricted". Assignment opens one task at edit, watching at view. |
| Saved views with stored filter configs | List, Board, Calendar, Timeline and Workload views; filters and sort remembered per list in the browser | Covers the daily need without a views table; shared server-side views can come later without schema changes. |
| `tsvector` full-text search | Title/key ILIKE search, exact key pinned first | Descriptions are short; title search found everything people looked for. Easy to upgrade later. |
| Attachments on tasks and comments | Attachments on tasks only | Comment attachments were never requested. |
| Named checklists (several per task) | One flat checklist per task | Matches how checklists were actually used in Vikunja. |
| Any number of replicas | **One backend replica** ([ADR 0001](adr/0001-single-backend-replica.md)) | The rate limiter, the stream limit and the background jobs (due automations, reminders, email, retention) run in-process. |
| Due-soon notifier under an advisory lock | Built-in due-soon reminders (once per task, person and due date) plus due-date automations | Reminders are on by default (`reminders.due_soon_days`); automations add custom reactions. |
| Polling only | Server-Sent Events from PostgreSQL `LISTEN/NOTIFY`, polling as fallback ([ADR 0006](adr/0006-live-updates-sse.md)) | Boards refresh within a second; triggers catch every change, including automations and jobs. |
| In-app Inbox only | Inbox plus email per person's choice, SMTP from Den or `SMTP_*` ([ADR 0007](adr/0007-settings-and-smtp-from-den.md)) | People asked to hear about assignments without keeping a tab open. |
| Custom fields per location | Custom fields per space | Same reason as statuses per space: one field set per space keeps boards, filters and imports coherent. |

Also added beyond the plan: one space per HR department created
automatically through `/api/internal/departments` (see Decided, below),
cursor pagination with `X-Next-Cursor`, per-user rate limiting, soft
delete with restore/purge, `task_ids` aliases across space-key renames
and cross-space moves, and a `tasks-dev` compose profile (ports
8161/3010, database `tasks_dev`) for developing next to the live one.

## Decisions so far

*(Original plan. Superseded where "Built" above says otherwise: statuses
have three categories and no per-list sets, views are not stored, live
updates use SSE, descriptions are plain text.)*

| Question | Decision |
| --- | --- |
| Structure | ClickUp's hierarchy: **Space → Folder (optional) → List → Task → Subtask**. A space usually maps to a department or a big project. |
| Statuses | Custom per space, overridable per list, each in a category (`not_started`, `active`, `done`, `closed`). Board columns are statuses. |
| Views | **List** and **Board** in the first slice; Calendar next; Gantt later. A view is a saved filter + grouping + sort on a space, folder or list. |
| Access | Same model as the wiki: spaces with grants to people or groups, lists/folders can add grants or be restricted. |
| Live updates | Polling (refetch every ~15 s and on window focus). Moves by others appear within seconds; no websocket. |
| Task description | BlockNote (same editor as the wiki), stored as JSON in Postgres, last write wins with a version check. No live co-editing. |
| Name | Tasks — `tasks.conservices.test`, Keycloak groups `tasks-*`, client `tasks-spa`. |

### What we take from ClickUp, and what we leave

| Taken | Left out (for now) |
| --- | --- |
| Space / Folder / List hierarchy | Docs (that is the wiki), Whiteboards, Chat |
| Custom statuses with categories, per space or list | Custom fields (first thing after the first slice) |
| List and Board views, saved per location, filters, group by, sort | Gantt, Timeline, Workload, Mind map, Table views |
| Task IDs with a space prefix (`SEC-142`) | Time tracking, Sprints, Goals, Dashboards |
| Subtasks and checklists | Automations, recurring tasks |
| Multiple assignees, watchers, priority (urgent/high/normal/low), start + due date, tags | Dependencies (blocking / waiting on) |
| Comments with @mentions, activity log on every task | Tasks in multiple lists, guests, task templates |
| "My Work" (everything assigned to me) and an in-app Inbox | Email-in to create tasks |

The hierarchy and statuses are what make ClickUp fit an organisation with
many departments; they are cheap to build and hard to add later, so they
go in from the start. Most of the left-out column is additive and can come
later without changing the schema.

## Stack (same as Learn unless noted)

- **Backend (`tasks-backend`):** Go 1.25, chi, pgx, Goose migrations
  (`cmd/migrate` one-shot), go-oidc, zerolog, `minio-go/v7` (Stash's
  `internal/files` client).
- **Frontend (`tasks-frontend`):** React 19 + Rsbuild + TanStack
  Router/Query/Table + Tailwind, the same Layout, brand, theme, login
  backdrop and Den-connection components as Learn. Drag and drop with
  `@dnd-kit` (board columns, list rows, sidebar tree). BlockNote for task
  descriptions and comments.

## Data model

**Hierarchy.** A space holds folders and lists; a folder holds lists; lists
can also sit directly in a space (ClickUp's "folderless lists"). Every task
belongs to exactly one list. Subtasks are tasks with a `parent_id`, live in
the same list, and are shown under their parent (one level in the UI;
nesting is not blocked in the schema).

**Statuses.** A space has an ordered set of statuses (name, colour,
category). A list either uses its space's statuses or has its own set.
Every set must have at least one `not_started` and one `closed` status.
Moving a task to a list with a different set maps its status by name, then
by category (first status of the same category), then to the first status;
the move dialog shows the mapping, as ClickUp does.

**Task IDs.** Each space has a short key (`SEC`, `REG`, `IT`) and a
counter; tasks get `SEC-142` on creation and keep it when moved inside the
space. Moving to another space gives a new ID; the old one keeps
redirecting.

**Ordering.** Manual order (rows in a list, cards in a board column) uses
fractional-index strings (`a0`, `a0V`, `a1`…), so a drag writes one row
and never renumbers the list.

**Views.** A view is `{type: list|board, filters, group_by, sort,
show_subtasks, show_closed}` stored per space, folder or list, plus one
default view per location. Filters: assignee (incl. "me"), status /
category, tag, priority, due (overdue, today, this week, none), text.
Group by: status (default for board), assignee, priority, due date, list.
Views on a space or folder show tasks from every list below it that the
person can see.

## Storage: what lives where

| Data | Where | Why |
| --- | --- | --- |
| Spaces, folders, lists, statuses, tasks, descriptions (BlockNote JSON), comments, checklists, activity, views, notifications | **Postgres** | All of it is queried, filtered and joined. |
| Attachments on tasks and comments | **S3** `tasks/files/<task_id>/<uuid><ext>` | Same as Stash. |
| Search text | **Postgres** (`tsvector` on title + description text + task ID) | Full-text search. |

Bucket `tasks-data-dev` goes into the operator's `FILES_S3_BUCKETS`.

## Access model

Module roles (in the role catalog so HR can assign them):

| Keycloak group | Role | May do |
| --- | --- | --- |
| `tasks-admin` | `organization_admin` | Every space, settings, Den pairing, delete forever. |
| `tasks-creator` | `space_creator` | Create new spaces. |

Every signed-in staff member can use Tasks (My Work, and anything shared
with them); there is no "tasks-user" group.

Location access (same model as the wiki, so both modules share the code
and the people/group picker):

- **Spaces** have grants to people or Keycloak groups (e.g. `dept-security`)
  with a level: `view`, `comment`, `edit` (create and change tasks) or
  `full` (also manage statuses, lists, views and sharing).
- **Folders and lists** inherit; they can **add** grants or be
  **restricted** (only their own grants apply to them and below).
- A person **assigned** to a task always gets `comment` on that task, even
  without access to its list, so a task can be handed to someone outside
  the space (ClickUp does the same).
- Effective access = nearest restricted ancestor or the space, highest
  matching grant; one recursive query. No access → 404.

## Schema sketch (first migration)

```text
organizations, users (issuer+subject), den_link    -- same as Learn
spaces        (id, org, name, key, icon, color, next_task_number,
               everyone_level, position, archived_at, deleted_at)
space_grants  (space, principal_kind user|group, principal, level)
folders       (id, space, name, position, restricted, archived_at, deleted_at)
lists         (id, space, folder null, name, color, position, restricted,
               own_statuses bool, archived_at, deleted_at)
location_grants (location_kind folder|list, location_id, principal_kind, principal, level)
statuses      (id, space null, list null, name, color,
               category not_started|active|done|closed, position)
tasks         (id, list, parent_id null, number, title, description_json,
               description_text, status, priority urgent|high|normal|low|null,
               start_at, due_at, position, created_by, completed_at,
               version, created_at, updated_at, archived_at, deleted_at,
               tsv tsvector)
task_ids      (space_key, number, task)              -- old IDs keep resolving after moves
task_assignees  (task, user)
task_watchers   (task, user)
tags (id, space, name, color), task_tags (task, tag)
checklists      (id, task, name, position)
checklist_items (id, checklist, text, done, assignee null, position)
comments      (id, task, author, body_json, body_text, created_at, edited_at, deleted_at)
attachments   (id, task, comment null, storage_key, filename, content_type,
               size_bytes, uploaded_by, created_at, deleted_at)
task_events   (id, task, actor, kind, data jsonb, created_at)  -- activity log
views         (id, location_kind space|folder|list, location_id, name,
               config jsonb, is_default, created_by, position)
notifications (id, user, task, kind assigned|mentioned|commented|status|due_soon,
               actor, created_at, read_at)
favorites     (user, location_kind, location_id, position)
```

## API (backend, first slice)

- `GET /api/me`, `GET /api/role-catalog`, `GET/POST/DELETE /api/den-link`,
  brand — same as Learn.
- Hierarchy: sidebar tree (spaces → folders → lists you can see), create /
  rename / move / archive / delete spaces, folders and lists, grants,
  restrict on/off, statuses (edit set, switch a list to its own set).
- Tasks: query (`location`, filters, group, sort, cursor pagination), get
  by id or `SEC-142`, create, patch (any field; `If-Match: version`, 409
  on conflict), move (list + position, with status mapping), bulk patch
  (status, assignees, tags, priority, due, move, archive, delete),
  subtasks, checklists, assignees, watchers, tags.
- Comments: list, create, edit, delete; @mentions notify and add the person
  as a watcher.
- Activity: `GET /api/tasks/{id}/activity` (events + comments interleaved).
- Views: CRUD per location.
- My Work: assigned to me, grouped Overdue / Today / Next 7 days / Later /
  No date.
- Inbox: notifications list, mark read / all read, unread count.
- Files: multipart upload through the backend (Stash's approach), download
  after the access check. 25 MB per file.
- Search: `GET /api/search?q=` over task IDs, titles and descriptions,
  filtered by access.

## Frontend

- Left sidebar (ClickUp layout): Home / My Work, Inbox (unread count),
  Favourites, then spaces → folders → lists as a tree with drag to
  reorder/move, "+" on every level, search (Ctrl+K, jumps straight to a
  task ID).
- Location header: breadcrumbs, view tabs (List, Board, saved views,
  "+ View"), filter / group / sort bar, "Me mode" toggle, Share.
- **List view:** rows grouped (by status by default) with collapsible
  groups, inline edit of status, assignees, due, priority; subtasks
  expandable under their parent; "+ New task" at the bottom of every group;
  multi-select with a bulk action bar.
- **Board view:** one column per status (or per group-by value), drag cards
  between columns and within a column, card shows ID, title, assignees,
  due, priority, tags, subtask/checklist progress.
- **Task panel** (opens as a side panel over any view, own URL
  `/t/SEC-142`): title, status, assignees, dates, priority, tags,
  description, subtasks, checklists, attachments, activity + comments on
  the right.
- Status editor per space / list (rename, colour, category, reorder).
- Brand, light/dark mode and sign-in backdrop from Den, like every module.

## Suite wiring

| Thing | Value |
| --- | --- |
| Directory | `tasks/` (`backend/`, `frontend/`, `infra/`, `docs/`), same layout as Learn |
| Compose profile | `tasks` (`tasks-migrate`, `tasks-backend`, `tasks-frontend`) |
| Host ports | API **8160**, UI **3009**, standalone Postgres **5441** |
| Database | `tasks` / user `tasks` (add to `infra/postgres/01-init-databases.sh`) |
| Bucket | `tasks-data-dev` (add to `minio-init` and to the operator's `FILES_S3_BUCKETS`) |
| Keycloak | client `tasks-spa`, groups `tasks-admin`, `tasks-creator` in `realm-conservices.json` and `apply-mvp.sh`; give `admin` `tasks-admin` |
| Proxy | `tasks.conservices.test` in the Caddyfile, network alias, hosts entry in `scripts/setup-local-dns.ps1` |
| HR | add `tasks=http://tasks-backend:8080/api/role-catalog` to `ROLE_CATALOG_SOURCES` |
| Den | pairing (Settings → Den connection), same as Learn |
| Smoke | `scripts/smoke-tasks.ps1` |
| README | root table rows + a Tasks note, `tasks/README.md` like Learn's |

*(Superseded by [ADR 0001](adr/0001-single-backend-replica.md): one
backend replica, `Recreate` strategy, no volume.)* ~~Stateless backend: any
number of replicas, `RollingUpdate`, no volume. The due-soon notifier runs
under a Postgres advisory lock so only one replica sends them.~~

## Milestones

1. **Scaffold.** Copy Learn's skeleton into `tasks/`, add Stash's S3
   client, wire everything in the table above. Done when the empty app
   signs in, pairs with Den and appears in HR's access picker.
2. **Hierarchy, statuses, access.** Spaces / folders / lists, the sidebar
   tree with drag and drop, status sets, grants and restriction, with
   authorization tests against a real Postgres (Learn's
   `TEST_DATABASE_URL` pattern). If the wiki's access code exists by then,
   extract it into a shared package instead of copying it.
3. **Tasks and List view.** Create / edit tasks, task IDs, the task panel,
   assignees, priority, dates, tags, subtasks, List view with grouping,
   filters, sort, inline edit, bulk actions, version conflicts.
4. **Board view.** Columns from statuses, drag and drop with fractional
   positions, moving between lists with status mapping, saved views.
5. **Collaboration.** Comments with @mentions, watchers, activity log,
   checklists, attachments to S3, Inbox notifications, due-soon reminders.
6. **My Work, search, favourites.** My Work page, Ctrl+K search and task-ID
   jump, favourites, archive and trash.
7. **Smoke script and docs.** `scripts/smoke-tasks.ps1` (create space with
   key → list → custom statuses → task → assign → move on board → comment
   with @mention → notification → restrict list → 404 for the other user),
   READMEs, operations notes.

## Out of the first slice

*(All built before 1.0.0 except task templates, sprints, table and mind-map
views, and the Desk/wiki/Rally integrations; see the changelog.)*

- **Custom fields** (text, number, dropdown, date, person, checkbox, URL) —
  the biggest gap against ClickUp; plan right after the first slice. Stored
  as `custom_fields` per location + `task_field_values`, filterable and
  groupable.
- Calendar view (next), Gantt / Timeline, Table view, Workload.
- Dependencies, recurring tasks, task templates, time tracking, sprints.
- Automations ("when status → done, notify X").
- Email notifications and digests (in-app Inbox only at first).
- Live updates over SSE instead of polling.
- Integrations: create a task from a Desk conversation, link tasks to wiki
  pages, Rally event checklists as a list template.
- Import from ClickUp / Vikunja / CSV.

## Open questions

Decided (November 2026):

- New spaces start **closed** (`everyone_level` null, same answer as the
  wiki); sharing happens per space or per list via grants (TASK-24, TASK-1).
- **One space per HR department, created automatically** when HR creates a
  department (TASK-25). HR calls `POST /api/internal/departments`
  (TASKS_SERVICE_TOKEN) on create/rename; the space is closed with the
  department's IdP group granted `edit`, and starts with the lists
  "To do", "Handoffs" and "Done archive". Inside their one space,
  departments can add as many lists and folders as they like.
- **No guests in the first year** (TASK-26). Everyone who needs Tasks gets
  a staff account through HR; vendors are represented by the staff member
  who owns the relationship. Revisit for next year's event if externals
  keep asking for access.

- **Calendar before 1.0** (maintainer, November 2026): a calendar per list
  and a cross-space calendar page, plus timeline and workload views.
- **HR's department hook** sends `{id, slug, name, group}` on create and
  save, and may add `"archived": true`; Tasks ignores unknown fields so HR
  can add fields first.

Still open: none.
