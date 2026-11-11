# ADR 0007: Module settings and SMTP come from Den, with Tasks' own fallback

- Status: accepted (November 2026); the Den side is to be confirmed

## Context

Retention periods and the mail server are organisation decisions: the
same SMTP account and data-protection rules apply to every module. When a
module is paired, Den is where admins set such things ("Module settings").
A standalone Tasks must still work.

## Decision

**Where a value comes from**, strongest first:

1. **Den**, when paired: Tasks reads the integration bundle it already
   syncs every minute (stored in `den_link.bundle`) and uses
   - `bundle.module_settings` — the same nested shape as Tasks' settings,
     for example `{"retention": {"trash_days": 30}, "reminders": {"due_soon_days": 1}}`;
     values outside the allowed range are ignored;
   - `bundle.smtp` — `{"host", "port", "username", "password", "from",
     "from_name", "security"}`.
2. **Tasks' own settings**: retention and reminders edited on *Settings*
   (table `module_settings`, audited); SMTP from the `SMTP_*` environment
   variables, so the password stays in the secret manager.
3. **Built-in defaults** (retention defaults are placeholders pending
   legal review).

A value Den sends is shown as "set in Den" and cannot be changed in Tasks
(`422`). Tasks reads the stored bundle directly rather than changing the
shared `denlink` package, which other work is replacing.

## Consequences

- Den must add `module_settings` and `smtp` to
  `GET /api/v1/integrations/bundle` in this shape; until it does, Tasks
  uses its own settings. If Den chooses another shape, only
  `internal/tasks/settings.go` (`denState`) changes.
- The bundle carries the SMTP password to every paired module; Den should
  only send it to modules that need mail.
- Emails go out from the Tasks backend, so its NetworkPolicy needs egress
  to the SMTP server whichever side configures it.
