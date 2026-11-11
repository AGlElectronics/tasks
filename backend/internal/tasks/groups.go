package tasks

import (
	"strings"

	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
)

// Group grants follow the shared group rule (groupmatch). subject_key holds
// groupmatch.NewGrant(subject).Stored: trimmed, one leading '/' removed,
// lower-cased; subject keeps what the admin typed. Matching compares
// groupmatch.Config.Key of the person's groups with the same transform of
// subject_key, done in SQL by tasks_group_key (migration
// 20261111000010_group_key_function.sql).

// subjectKey is the subject_key column for a grant subject.
func subjectKey(subjectType, subject string) string {
	if subjectType == "group" {
		return groupmatch.NewGrant(subject).Stored
	}
	return subject
}

// groupMatchArgs are the SQL arguments for tasks_group_key(subject_key,
// prefix, mode) = ANY(keys): the person's group keys under c, and c's
// prefix and mode in the form groupmatch compares them.
func groupMatchArgs(c groupmatch.Config, groups []string) (keys []string, prefix, mode string) {
	keys = make([]string, 0, len(groups))
	for _, g := range groups {
		if k := c.Key(g); k != "" {
			keys = append(keys, k)
		}
	}
	mode = string(groupmatch.ModeFull)
	if c.Mode == groupmatch.ModeLast {
		mode = string(groupmatch.ModeLast)
	}
	return keys, strings.TrimSuffix(groupmatch.Normalize(c.Prefix), "/"), mode
}
