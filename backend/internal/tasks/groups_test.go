package tasks

import "testing"

func TestGroupKey(t *testing.T) {
	cases := map[string]string{
		"dept-security":          "dept-security",
		"  /Dept-Security ":      "dept-security",
		"/departments/Security":  "departments/security",
		"departments/security/":  "departments/security/",
		"//double":               "/double",
		"":                       "",
		"   ":                    "",
	}
	for in, want := range cases {
		if got := GroupKey(in); got != want {
			t.Errorf("GroupKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := groupKeys([]string{"/A", "", " b "}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("groupKeys dropped or kept the wrong names: %v", got)
	}
	if subjectKey("user", "ABC") != "ABC" || subjectKey("group", "/ABC") != "abc" {
		t.Error("subjectKey: users stay verbatim, groups are normalised")
	}
}
