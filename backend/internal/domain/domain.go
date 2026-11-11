// Package domain holds the task-tracking vocabulary — role names,
// permission identifiers and fixed value sets — used by both the service
// layer and the HTTP layer. Keeping these as typed constants prevents
// stringly typed drift between tables, APIs, and the access config file.
package domain

// Role names recognized by default. The access config file (TOML) may add
// more roles or redefine the permission set attached to any of these.
const (
	// RoleOrganizationAdmin manages every space and the module settings.
	RoleOrganizationAdmin = "organization_admin"
	// RoleSpaceCreator may create spaces (and manages the ones they create).
	RoleSpaceCreator = "space_creator"
)

// Status categories. Board columns are statuses; the category says what a
// status means for "done" and for My Work.
const (
	CategoryNotStarted = "not_started"
	CategoryActive     = "active"
	CategoryDone       = "done"
)

// Priorities, most urgent first.
var Priorities = []string{"urgent", "high", "normal", "low"}

// Permission identifiers. Working on tasks needs no permission, only access
// to the space or list: its everyone level, a grant to the person or one of
// their groups, or being assigned to or watching the task.
const (
	// PermSpaceManage: edit and delete every space, its lists and statuses,
	// regardless of per-space grants.
	PermSpaceManage = "space.manage"
	// PermSpaceCreate: create spaces; manage the spaces one created.
	PermSpaceCreate = "space.create"
	// PermSettingsManage: module settings, including the Den pairing.
	PermSettingsManage = "settings.manage"
)

// AllPermissions returns every permission the access-config validator will
// accept. The access config file may only reference identifiers listed here;
// unknown names fail configuration load with a clear error.
func AllPermissions() []string {
	return []string{
		PermSpaceManage,
		PermSpaceCreate,
		PermSettingsManage,
	}
}

// DefaultRolePermissions is the built-in role → permission catalog applied
// when the access config file omits a role (or isn't configured at all).
func DefaultRolePermissions() map[string][]string {
	return map[string][]string{
		RoleOrganizationAdmin: AllPermissions(),
		RoleSpaceCreator:      {PermSpaceCreate},
	}
}
