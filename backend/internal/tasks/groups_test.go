package tasks

import (
	"slices"
	"testing"

	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
)

// subject_key must stay exactly what migration 20261111000001 backfilled.
func TestSubjectKey(t *testing.T) {
	cases := map[string]string{
		"dept-security":         "dept-security",
		"  /Dept-Security ":     "dept-security",
		"/departments/Security": "departments/security",
		"departments/security/": "departments/security/",
		"//double":              "/double",
		"":                      "",
		"   ":                   "",
	}
	for in, want := range cases {
		if got := subjectKey("group", in); got != want {
			t.Errorf("subjectKey(group, %q) = %q, want %q", in, got, want)
		}
	}
	if subjectKey("user", "ABC") != "ABC" {
		t.Error("user subjects stay verbatim")
	}
}

func TestGroupMatchArgs(t *testing.T) {
	keys, prefix, mode := groupMatchArgs(groupmatch.Config{}, []string{"/A", "", " b "})
	if !slices.Equal(keys, []string{"a", "b"}) || prefix != "" || mode != "full" {
		t.Errorf("default rule: keys %v prefix %q mode %q", keys, prefix, mode)
	}
	c := groupmatch.Config{Mode: groupmatch.ModeLast, Prefix: "/Con2026/"}
	keys, prefix, mode = groupMatchArgs(c, []string{"/con2026/departments/Security", "/con2026", "/other/x"})
	if !slices.Equal(keys, []string{"security", "x"}) || prefix != "con2026" || mode != "last" {
		t.Errorf("prefix+last: keys %v prefix %q mode %q", keys, prefix, mode)
	}
}
