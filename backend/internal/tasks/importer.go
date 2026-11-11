package tasks

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// Import formats.
const (
	ImportCSV     = "csv"     // a header row with columns named below
	ImportClickUp = "clickup" // ClickUp's CSV export
	ImportVikunja = "vikunja" // Vikunja's data.json export
)

// Import limits.
const (
	MaxImportBytes = 5 << 20
	MaxImportTasks = 2000
	maxWarnings    = 100
	previewRows    = 50
)

// ImportInput is POST /api/v1/lists/{list}/import.
type ImportInput struct {
	Format  string `json:"format"`
	Content string `json:"content"`
	// DryRun checks and previews without writing anything.
	DryRun bool `json:"dry_run"`
}

// ImportRow is one task as it will be created.
type ImportRow struct {
	Line        int      `json:"line"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Priority    *string  `json:"priority"`
	StartAt     *string  `json:"start_at"`
	DueAt       *string  `json:"due_at"`
	Assignees   []string `json:"assignees"`
	Tags        []string `json:"tags"`

	statusID    uuid.UUID
	category    string
	assigneeIDs []uuid.UUID
	tagNames    []string
}

// ImportResult says what an import did (or would do, on a dry run).
type ImportResult struct {
	DryRun   bool        `json:"dry_run"`
	Created  int         `json:"created"`
	Skipped  int         `json:"skipped"`
	NewTags  []string    `json:"new_tags"`
	Warnings []string    `json:"warnings"`
	Preview  []ImportRow `json:"preview"`
}

// rawRow is a parsed source row before it is matched to the space.
type rawRow struct {
	line                       int
	title, description, status string
	priority, start, due       string
	done                       bool
	assignees, tags            []string
}

var csvColumns = map[string][]string{
	"title":       {"title", "name", "task", "task name", "summary"},
	"description": {"description", "task content", "content", "notes", "details"},
	"status":      {"status", "state"},
	"priority":    {"priority"},
	"due":         {"due", "due date", "due_at", "deadline"},
	"start":       {"start", "start date", "start_at"},
	"assignees":   {"assignee", "assignees", "assignee email", "assignee emails", "owner"},
	"tags":        {"tags", "tag", "labels", "label"},
	"done":        {"done", "completed"},
}

func splitList(s string) []string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s), "["), "]"))
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.Trim(strings.TrimSpace(p), `"'`); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseCSV(content string) ([]rawRow, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\ufeff")))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, httperror.Validation("the file is not valid CSV: "+err.Error(), nil)
	}
	if len(records) == 0 {
		return nil, httperror.Validation("the file is empty", nil)
	}
	col := map[string]int{}
	for i, h := range records[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		for field, names := range csvColumns {
			for _, n := range names {
				if h == n {
					if _, seen := col[field]; !seen {
						col[field] = i
					}
				}
			}
		}
	}
	if _, ok := col["title"]; !ok {
		return nil, httperror.Validation("no title column: name one column Title, Name or Task Name", nil)
	}
	get := func(rec []string, field string) string {
		if i, ok := col[field]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	var rows []rawRow
	for n, rec := range records[1:] {
		done := strings.ToLower(get(rec, "done"))
		rows = append(rows, rawRow{
			line: n + 2, title: get(rec, "title"), description: get(rec, "description"), status: get(rec, "status"),
			priority: get(rec, "priority"), start: get(rec, "start"), due: get(rec, "due"),
			done:      done == "true" || done == "yes" || done == "1" || done == "x",
			assignees: splitList(get(rec, "assignees")), tags: splitList(get(rec, "tags")),
		})
	}
	return rows, nil
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// stripHTML turns Vikunja's HTML descriptions into plain text.
func stripHTML(s string) string {
	s = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</li>`).ReplaceAllString(s, "\n")
	return strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(s, "")))
}

type vikunjaTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Done        bool   `json:"done"`
	DueDate     string `json:"due_date"`
	StartDate   string `json:"start_date"`
	Priority    int    `json:"priority"`
	Labels      []struct {
		Title string `json:"title"`
	} `json:"labels"`
	Assignees []struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Name     string `json:"name"`
	} `json:"assignees"`
}

type vikunjaProject struct {
	Title string        `json:"title"`
	Tasks []vikunjaTask `json:"tasks"`
	// Newer exports nest tasks in buckets.
	Buckets []struct {
		Tasks []vikunjaTask `json:"tasks"`
	} `json:"buckets"`
}

func parseVikunja(content string) ([]rawRow, error) {
	var projects []vikunjaProject
	if err := json.Unmarshal([]byte(content), &projects); err != nil {
		var one vikunjaProject
		if err2 := json.Unmarshal([]byte(content), &one); err2 != nil {
			return nil, httperror.Validation("the file is not a Vikunja export (data.json): "+err.Error(), nil)
		}
		projects = []vikunjaProject{one}
	}
	// A bare array of tasks also parses as projects with titles only.
	var bare []vikunjaTask
	if json.Unmarshal([]byte(content), &bare) == nil {
		isBare := len(bare) > 0
		for _, p := range projects {
			if len(p.Tasks) > 0 || len(p.Buckets) > 0 {
				isBare = false
			}
		}
		if isBare {
			projects = []vikunjaProject{{Tasks: bare}}
		}
	}
	prio := map[int]string{1: "low", 2: "normal", 3: "high", 4: "urgent", 5: "urgent"}
	var rows []rawRow
	n := 0
	add := func(t vikunjaTask) {
		n++
		r := rawRow{line: n, title: strings.TrimSpace(t.Title), description: stripHTML(t.Description), done: t.Done,
			priority: prio[t.Priority], due: t.DueDate, start: t.StartDate}
		for _, l := range t.Labels {
			r.tags = append(r.tags, l.Title)
		}
		for _, a := range t.Assignees {
			switch {
			case a.Email != "":
				r.assignees = append(r.assignees, a.Email)
			case a.Name != "":
				r.assignees = append(r.assignees, a.Name)
			default:
				r.assignees = append(r.assignees, a.Username)
			}
		}
		rows = append(rows, r)
	}
	for _, p := range projects {
		for _, t := range p.Tasks {
			add(t)
		}
		for _, b := range p.Buckets {
			for _, t := range b.Tasks {
				add(t)
			}
		}
	}
	return rows, nil
}

var ordinal = regexp.MustCompile(`(\d+)(st|nd|rd|th)\b`)

// parseImportDate understands ISO dates, RFC 3339, Unix milliseconds
// (ClickUp), day/month/year and ClickUp's "Tuesday, March 5th 2024".
// Vikunja's zero date means "none".
func parseImportDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return "", true
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 1e11 {
		return time.UnixMilli(ms).UTC().Format("2006-01-02"), true
	}
	clean := ordinal.ReplaceAllString(s, "$1")
	for _, layout := range []string{
		"2006-01-02", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04",
		"02/01/2006", "2/1/2006", "02.01.2006", "2.1.2006",
		"Monday, January 2 2006", "Monday, January 2 2006, 3:04 pm", "January 2, 2006", "Jan 2, 2006", "2 January 2006",
	} {
		if t, err := time.Parse(layout, clean); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

func normPriority(s string) (*string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	m := map[string]string{
		"urgent": "urgent", "1": "urgent", "high": "high", "2": "high", "normal": "normal", "3": "normal",
		"medium": "normal", "low": "low", "4": "low",
	}
	if s == "" {
		return nil, true
	}
	if p, ok := m[s]; ok {
		return &p, true
	}
	return nil, false
}

var doneNames = map[string]bool{"done": true, "closed": true, "complete": true, "completed": true, "finished": true}

// ImportTasks creates tasks in a list from a ClickUp CSV, a Vikunja JSON
// export or a plain CSV. Statuses match by name (else done-looking names
// and Vikunja's done flag map to the first done status, the rest to the
// first not-started one); assignees match people who have signed in to
// Tasks by email or name; tags match the space's tags by name and are
// created when canCreateTags (the caller manages the space). With DryRun
// nothing is written. Everything is created in one transaction.
func (s *Service) ImportTasks(ctx context.Context, actor, listID uuid.UUID, in ImportInput, canCreateTags bool) (ImportResult, error) {
	res := ImportResult{DryRun: in.DryRun, NewTags: []string{}, Warnings: []string{}, Preview: []ImportRow{}}
	if len(in.Content) > MaxImportBytes {
		return res, httperror.Validation("the file is larger than 5 MB", nil)
	}
	var rows []rawRow
	var err error
	switch in.Format {
	case ImportCSV, ImportClickUp:
		rows, err = parseCSV(in.Content)
	case ImportVikunja:
		rows, err = parseVikunja(in.Content)
	default:
		return res, httperror.Validation("invalid import", map[string]string{"format": "must be csv, clickup or vikunja"})
	}
	if err != nil {
		return res, err
	}
	if len(rows) > MaxImportTasks {
		return res, httperror.Validation(fmt.Sprintf("at most %d tasks per import; split the file", MaxImportTasks), nil)
	}
	warn := func(format string, args ...any) {
		if len(res.Warnings) < maxWarnings {
			res.Warnings = append(res.Warnings, fmt.Sprintf(format, args...))
		}
	}

	l, err := s.GetList(ctx, listID)
	if err != nil {
		return res, err
	}
	if l.Archived {
		return res, httperror.Conflict("the list is archived")
	}
	statuses, err := s.statuses(ctx, s.pool, l.SpaceID)
	if err != nil {
		return res, err
	}
	var firstOpen, firstDone *Status
	byName := map[string]Status{}
	for i := range statuses {
		st := statuses[i]
		byName[strings.ToLower(st.Name)] = st
		if st.Category == "done" && firstDone == nil {
			firstDone = &statuses[i]
		}
		if st.Category == "not_started" && firstOpen == nil {
			firstOpen = &statuses[i]
		}
	}
	if firstOpen == nil {
		firstOpen = &statuses[0]
	}
	tags, err := s.SpaceTags(ctx, l.SpaceID)
	if err != nil {
		return res, err
	}
	tagByName := map[string]uuid.UUID{}
	for _, t := range tags {
		tagByName[strings.ToLower(t.Name)] = t.ID
	}
	people := map[string]uuid.UUID{}
	prow, err := s.pool.Query(ctx, `SELECT id, lower(COALESCE(email, '')), lower(display_name) FROM users`)
	if err != nil {
		return res, err
	}
	for prow.Next() {
		var id uuid.UUID
		var email, name string
		if err := prow.Scan(&id, &email, &name); err != nil {
			prow.Close()
			return res, err
		}
		if email != "" {
			people[email] = id
		}
		if _, taken := people[name]; !taken {
			people[name] = id
		}
	}
	prow.Close()

	newTags := map[string]bool{}
	var ready []ImportRow
	for _, r := range rows {
		if r.title == "" {
			res.Skipped++
			warn("line %d: no title, skipped", r.line)
			continue
		}
		if len(r.title) > maxTitleLen {
			r.title = r.title[:maxTitleLen]
			warn("line %d: title shortened to %d characters", r.line, maxTitleLen)
		}
		if len(r.description) > maxDescriptionLen {
			r.description = r.description[:maxDescriptionLen]
			warn("line %d: description shortened", r.line)
		}
		row := ImportRow{Line: r.line, Title: r.title, Description: r.description, Assignees: []string{}, Tags: []string{}}
		st, ok := byName[strings.ToLower(strings.TrimSpace(r.status))]
		switch {
		case ok:
		case (r.done || doneNames[strings.ToLower(strings.TrimSpace(r.status))]) && firstDone != nil:
			st = *firstDone
		default:
			st = *firstOpen
			if r.status != "" {
				warn("line %d: status %q is not in this space, used %q", r.line, r.status, st.Name)
			}
		}
		row.Status, row.statusID, row.category = st.Name, st.ID, st.Category
		if p, ok := normPriority(r.priority); ok {
			row.Priority = p
		} else {
			warn("line %d: priority %q not understood, left empty", r.line, r.priority)
		}
		for name, src := range map[string]string{"due": r.due, "start": r.start} {
			d, ok := parseImportDate(src)
			if !ok {
				warn("line %d: %s date %q not understood, left empty", r.line, name, src)
				continue
			}
			if d == "" {
				continue
			}
			dd := d
			if name == "due" {
				row.DueAt = &dd
			} else {
				row.StartAt = &dd
			}
		}
		seen := map[uuid.UUID]bool{}
		for _, a := range r.assignees {
			id, ok := people[strings.ToLower(strings.TrimSpace(a))]
			if !ok {
				warn("line %d: %q has never signed in to Tasks, not assigned", r.line, a)
				continue
			}
			if !seen[id] {
				seen[id] = true
				row.assigneeIDs = append(row.assigneeIDs, id)
				row.Assignees = append(row.Assignees, a)
			}
		}
		for _, t := range r.tags {
			t = strings.TrimSpace(t)
			if t == "" || len(t) > 100 {
				continue
			}
			key := strings.ToLower(t)
			if _, ok := tagByName[key]; !ok {
				if !canCreateTags {
					warn("line %d: tag %q does not exist and only space managers can create tags", r.line, t)
					continue
				}
				if !newTags[key] {
					newTags[key] = true
					res.NewTags = append(res.NewTags, t)
				}
			}
			row.tagNames = append(row.tagNames, t)
			row.Tags = append(row.Tags, t)
		}
		ready = append(ready, row)
		if len(res.Preview) < previewRows {
			res.Preview = append(res.Preview, row)
		}
	}
	res.Created = len(ready)
	if in.DryRun || len(ready) == 0 {
		return res, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, name := range res.NewTags {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tags (space_id, name) VALUES ($1, $2) RETURNING id`, l.SpaceID, name).Scan(&id); err != nil {
			return res, err
		}
		tagByName[strings.ToLower(name)] = id
	}
	var base int
	if err := tx.QueryRow(ctx, `UPDATE spaces SET next_number = next_number + $2 WHERE id = $1 RETURNING next_number - $2`,
		l.SpaceID, len(ready)).Scan(&base); err != nil {
		return res, err
	}
	var maxPos float64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position), 0) FROM tasks WHERE list_id = $1`, listID).Scan(&maxPos); err != nil {
		return res, err
	}
	source := map[string]any{"source": "import", "format": in.Format}
	for i, row := range ready {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO tasks (space_id, list_id, number, title, description, status_id, priority, start_at, due_at,
			                   position, created_by, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, CASE WHEN $12 = 'done' THEN now() END)
			RETURNING id`, l.SpaceID, listID, base+i, row.Title, row.Description, row.statusID, row.Priority,
			row.StartAt, row.DueAt, maxPos+float64(i+1)*1024, actor, row.category).Scan(&id); err != nil {
			return res, err
		}
		for _, uid := range row.assigneeIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO task_assignees (task_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid); err != nil {
				return res, err
			}
		}
		for _, t := range row.tagNames {
			if _, err := tx.Exec(ctx, `INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, tagByName[strings.ToLower(t)]); err != nil {
				return res, err
			}
		}
		if err := addEvent(ctx, tx, id, actor, "created", source); err != nil {
			return res, err
		}
	}
	if err := writeAudit(ctx, tx, &actor, "list", listID, "imported", map[string]any{
		"name": l.Name, "format": in.Format, "tasks": len(ready), "new_tags": len(res.NewTags),
	}); err != nil {
		return res, err
	}
	return res, tx.Commit(ctx)
}
