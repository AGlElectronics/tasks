# ADR 0005: Grants, private lists and how group names match

- Status: accepted (November 2026)

## Context

Departments need closed spaces; teams need to share a single list with
another team; IdP groups arrive in different spellings (Keycloak sends
full paths like `/Dept-Security`, admins type `dept-security`).

## Decision

**Access** is a level (`view`, `edit`, `manage`), the strongest of: the
space's everyone level (`null` = closed, the default for new spaces), a
grant to the person or one of their groups on the space, a grant on the
list, organisation permissions (`space.manage`, or `space.create` plus
being the creator), and — for one task — being assigned (edit) or
watching (view). A **private list** hides from space-level viewers; only
list grants and space managers see it. This replaces the plan's
"restriction" with something people understand. Reads below `view` answer
`404`, writes below the needed level `403`.

**Group names** follow the rule every Con Services module will share
(`shared/go/groupmatch`): trim spaces, remove one leading `/`, compare
case-insensitively, and compare the whole path (`/departments/ops`
matches `departments/ops`, not `ops`). Each grant stores the name as typed
(`subject`, shown in the UI) and the normalised form (`subject_key`, used
for matching and for the one-grant-per-group unique index).

Update (November 2026): Tasks now uses the shared package
(`internal/groupmatch`, synced by `scripts/sync-shared`). `subject_key` is
`groupmatch.NewGrant(subject).Stored`, which is the same normalised form as
before, so stored grants did not change. Groups are read only from
`OIDC_GROUPS_CLAIM`; realm roles are no longer merged in.
`OIDC_GROUP_PREFIX` and `OIDC_GROUP_MATCH=last` are applied at match time:
the person's groups become `Config.Key` values in Go, and the stored
`subject_key` goes through the SQL function `tasks_group_key(key, prefix,
mode)` (migration `20261111000010_group_key_function.sql`), which applies
the same prefix and last-segment rule. A test checks that the SQL function
and the Go package agree. With the defaults (no prefix, `full`) the
function returns the key unchanged.

## Consequences

- The migration `20261111000001_grant_subject_key.sql` backfilled
  `subject_key` and merged grants that differed only in case or a leading
  `/`, keeping the strongest level.
- Every grant change is in the audit log.
- Changing the matching rule later (for example last-segment matching) is
  a configuration of the shared package, not a data migration.
- `tasks_group_key` duplicates the prefix and last-segment rule of
  `groupmatch.Config.Key` in SQL, because grants are matched in queries.
  When the shared package changes that rule, the SQL function needs a new
  migration; the agreement test fails until it has one.
