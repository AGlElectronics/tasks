package tasks

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseTaskFilter(t *testing.T) {
	id := uuid.New()
	q := url.Values{
		"priority": {"Urgent, high", "none"},
		"tag":      {id.String(), "Venue,press", ""},
	}
	f, err := ParseTaskFilter(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.Priorities, ",") != "urgent,high,none" {
		t.Fatalf("priorities: %v", f.Priorities)
	}
	if len(f.TagIDs) != 1 || f.TagIDs[0] != id || strings.Join(f.TagNames, ",") != "venue,press" {
		t.Fatalf("tags: %v %v", f.TagIDs, f.TagNames)
	}
	if f.IsZero() {
		t.Fatal("filter is not empty")
	}

	for _, bad := range []url.Values{
		{"priority": {"asap"}},
		{"priority": {"urgent,,soon"}},
		{"tag": {strings.Repeat("a,", maxFilterValues+1)}},
	} {
		if _, err := ParseTaskFilter(bad); err == nil {
			t.Fatalf("%v should be rejected", bad)
		}
	}

	zero, err := ParseTaskFilter(url.Values{})
	if err != nil || !zero.IsZero() {
		t.Fatalf("empty query: %v %v", zero, err)
	}
}

func TestTaskFilterSQL(t *testing.T) {
	if sql, args := (TaskFilter{}).sql("t", 4); sql != "" || len(args) != 0 {
		t.Fatalf("zero filter adds nothing: %q %v", sql, args)
	}
	f := TaskFilter{Priorities: []string{"urgent", PriorityNone}, TagNames: []string{"venue"}}
	sql, args := f.sql("t", 4)
	// priority takes $4, the label name takes $5; "none" needs no argument.
	if len(args) != 2 || !strings.Contains(sql, "t.priority = ANY($4::text[])") ||
		!strings.Contains(sql, "t.priority IS NULL") || !strings.Contains(sql, "lower(flg.name) = ANY($5::text[])") {
		t.Fatalf("sql %q args %v", sql, args)
	}
	only, args := (TaskFilter{Priorities: []string{PriorityNone}}).sql("t", 2)
	if len(args) != 0 || !strings.Contains(only, "t.priority IS NULL") {
		t.Fatalf("none only: %q %v", only, args)
	}
}
