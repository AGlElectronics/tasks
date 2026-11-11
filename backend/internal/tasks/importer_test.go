package tasks

import "testing"

func TestParseImportDate(t *testing.T) {
	cases := map[string]string{
		"2027-03-05":             "2027-03-05",
		"2027-03-05T10:00:00Z":   "2027-03-05",
		"05/03/2027":             "2027-03-05",
		"5.3.2027":               "2027-03-05",
		"Friday, March 5th 2027": "2027-03-05",
		"March 5, 2027":          "2027-03-05",
		"1804204800000":          "2027-03-05",
		"0001-01-01T00:00:00Z":   "",
		"":                       "",
	}
	for in, want := range cases {
		got, ok := parseImportDate(in)
		if !ok || got != want {
			t.Errorf("parseImportDate(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := parseImportDate("next week"); ok {
		t.Error("unreadable dates are reported")
	}
}

func TestNormPriorityAndLists(t *testing.T) {
	for in, want := range map[string]string{"Urgent": "urgent", "2": "high", "medium": "normal", "4": "low"} {
		if p, ok := normPriority(in); !ok || *p != want {
			t.Errorf("normPriority(%q) = %v", in, p)
		}
	}
	if p, ok := normPriority(""); !ok || p != nil {
		t.Error("empty priority is none")
	}
	if _, ok := normPriority("whenever"); ok {
		t.Error("unknown priority is reported")
	}
	if got := splitList(`[ann@example.org, "bo@example.org"]`); len(got) != 2 || got[1] != "bo@example.org" {
		t.Errorf("splitList: %v", got)
	}
	if got := stripHTML("<p>Hello&nbsp;<b>world</b></p><p>second</p>"); got != "Hello\u00a0world\nsecond" {
		t.Errorf("stripHTML: %q", got)
	}
}
