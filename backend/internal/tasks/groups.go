package tasks

import "strings"

// GroupKey is the form an IdP group name is stored and compared in: spaces
// trimmed, one leading '/' (Keycloak's full-path form) removed,
// lower-cased. The whole path must match, so "/departments/security"
// matches "departments/security" but not "security".
//
// TODO(shared/groupmatch): replace with groupmatch.Config.Key once the
// shared module is adopted (it adds OIDC_GROUP_PREFIX and OIDC_GROUP_MATCH).
func GroupKey(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/"))
}

// groupKeys normalises a person's groups for matching against subject_key.
func groupKeys(groups []string) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		if k := GroupKey(g); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// subjectKey is the subject_key column for a grant subject.
func subjectKey(subjectType, subject string) string {
	if subjectType == "group" {
		return GroupKey(subject)
	}
	return subject
}
