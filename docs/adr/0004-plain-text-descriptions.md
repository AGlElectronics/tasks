# ADR 0004: Plain-text descriptions and comments

- Status: accepted (November 2026)

## Context

The plan proposed BlockNote (the wiki's editor) for task descriptions and
comments, stored as JSON. Feedback from Vikunja users was that tasks had
"too many fields" and that long documents belong in the wiki.

## Decision

Descriptions (up to 100,000 characters) and comments (up to 20,000) are
plain text, shown with line breaks kept. Long-form content lives in the
wiki and is linked from the task. Imports convert HTML (Vikunja) to plain
text.

## Consequences

- No editor dependency in the web app, no JSON migrations of rich text.
- Search and email can use the text as it is.
- Switching to rich text later needs a migration of existing text (wrap
  each description in one paragraph block); the API field stays a string.
