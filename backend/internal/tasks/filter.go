package tasks

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// Labels and urgency. A label is a space tag (tags.go) and urgency is the
// task priority (domain.Priorities); this file lets list, My Work and Inbox
// reads narrow down by them and lets My Work and the portal to-dos order by
// urgency. Nothing new is stored: the filters read task_tags and
// tasks.priority.

// PriorityNone matches tasks without a priority in a filter.
const PriorityNone = "none"

// maxFilterValues caps how many urgencies or labels one request may name.
const maxFilterValues = 20

// TaskFilter narrows a task read. The zero value matches everything. A task
// matches when its priority is one of Priorities AND it wears at least one
// of the Tags.
type TaskFilter struct {
	// Priorities: urgent, high, normal, low, or "none" for no priority.
	Priorities []string
	// TagIDs and TagNames name labels. Names match case-insensitively, so
	// a name works across spaces (My Work, Inbox); an id is one space's tag.
	TagIDs   []uuid.UUID
	TagNames []string
}

// IsZero reports whether the filter matches everything.
func (f TaskFilter) IsZero() bool {
	return len(f.Priorities) == 0 && len(f.TagIDs) == 0 && len(f.TagNames) == 0
}

// ParseTaskFilter reads ?priority= and ?tag= from a query string. Both
// accept repeated parameters and comma-separated lists
// (?priority=urgent,high&tag=Venue&tag=Press).
func ParseTaskFilter(q url.Values) (TaskFilter, error) {
	var f TaskFilter
	issues := map[string]string{}

	for _, raw := range splitValues(q["priority"]) {
		p := strings.ToLower(raw)
		if p != PriorityNone && !validPriority(&p) {
			issues["priority"] = "must be urgent, high, normal, low or none"
			break
		}
		f.Priorities = append(f.Priorities, p)
	}
	for _, raw := range splitValues(q["tag"]) {
		if id, err := uuid.Parse(raw); err == nil {
			f.TagIDs = append(f.TagIDs, id)
		} else {
			f.TagNames = append(f.TagNames, strings.ToLower(raw))
		}
	}
	if len(f.Priorities) > maxFilterValues {
		issues["priority"] = "too many values"
	}
	if len(f.TagIDs)+len(f.TagNames) > maxFilterValues {
		issues["tag"] = "too many values"
	}
	if len(issues) > 0 {
		return TaskFilter{}, httperror.Validation("invalid filter", issues)
	}
	return f, nil
}

func splitValues(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// sql returns " AND …" predicates on the task row aliased alias, numbering
// its placeholders from $first, and the matching arguments. Only the
// predicates in use are emitted so no placeholder goes unreferenced.
func (f TaskFilter) sql(alias string, first int) (string, []any) {
	var sb strings.Builder
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", first+len(args)-1)
	}

	if len(f.Priorities) > 0 {
		var ranks []string
		withNone := false
		for _, p := range f.Priorities {
			if p == PriorityNone {
				withNone = true
			} else {
				ranks = append(ranks, p)
			}
		}
		var parts []string
		if len(ranks) > 0 {
			parts = append(parts, fmt.Sprintf("%s.priority = ANY(%s::text[])", alias, next(ranks)))
		}
		if withNone {
			parts = append(parts, alias+".priority IS NULL")
		}
		fmt.Fprintf(&sb, " AND (%s)", strings.Join(parts, " OR "))
	}
	if len(f.TagIDs) > 0 || len(f.TagNames) > 0 {
		var parts []string
		if len(f.TagIDs) > 0 {
			parts = append(parts, fmt.Sprintf("flg.id = ANY(%s::uuid[])", next(f.TagIDs)))
		}
		if len(f.TagNames) > 0 {
			parts = append(parts, fmt.Sprintf("lower(flg.name) = ANY(%s::text[])", next(f.TagNames)))
		}
		fmt.Fprintf(&sb, ` AND EXISTS (SELECT 1 FROM task_tags flt JOIN tags flg ON flg.id = flt.tag_id
		   WHERE flt.task_id = %s.id AND (%s))`, alias, strings.Join(parts, " OR "))
	}
	return sb.String(), args
}

// urgencyRank orders tasks most urgent first with unset priorities last,
// the same order the web app sorts by. alias is the task row.
func urgencyRank(alias string) string {
	return fmt.Sprintf(`CASE %[1]s.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1
		WHEN 'normal' THEN 2 WHEN 'low' THEN 3 ELSE 4 END`, alias)
}

// SortUrgency is the MyWork sort that puts urgent tasks first.
const SortUrgency = "urgency"

// ValidMyWorkSort reports whether sort is "", "due" or "urgency".
func ValidMyWorkSort(sort string) bool {
	return sort == "" || sort == "due" || sort == SortUrgency
}
