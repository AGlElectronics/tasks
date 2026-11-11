package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
)

type roleCatalogEntry struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Group        string   `json:"group"`
	GroupDisplay string   `json:"group_display"`
	Assignable   bool     `json:"assignable"`
	Default      bool     `json:"default"`
	Scope        string   `json:"scope"`
	Privileged   bool     `json:"privileged"`
	Dynamic      bool     `json:"dynamic"`
	Permissions  []string `json:"permissions"`
}

type roleCatalog struct {
	Module      string             `json:"module"`
	DisplayName string             `json:"display_name"`
	Description string             `json:"description"`
	Revision    string             `json:"revision"`
	Roles       []roleCatalogEntry `json:"roles"`
}

var tasksRoleDescriptions = map[string]string{
	domain.RoleOrganizationAdmin: "Sees and manages every space regardless of sharing, plus statuses, lists and the module settings.",
	domain.RoleSpaceCreator:      "Creates new spaces and manages the spaces they created. Other access comes from per-space and per-list sharing, not from a role.",
}

// privilegedPermissions make a role admin-type in the catalog: they see
// every space or change the module's settings and its Den pairing.
var privilegedPermissions = map[string]bool{
	domain.PermSpaceManage:    true,
	domain.PermSettingsManage: true,
}

// buildRoleCatalog lists every role bound to an IdP group in the access
// config (one entry per binding); those are what HR can assign. Tasks has no
// dynamic roles: per-space sharing is not a role. The access config is
// fixed at start-up, so the revision is a hash of the catalog.
func buildRoleCatalog(access *auth.AccessConfig) roleCatalog {
	roles := make([]roleCatalogEntry, 0)
	bindings := access.Bindings()
	for _, role := range access.Roles() {
		set := access.PermissionsForRole(role)
		perms := make([]string, 0, len(set))
		privileged := false
		for p := range set {
			perms = append(perms, p)
			privileged = privileged || privilegedPermissions[p]
		}
		sort.Strings(perms)
		desc := access.RoleDescription(role)
		if desc == "" {
			desc = tasksRoleDescriptions[role]
		}
		for _, b := range bindings {
			if b.Role != role {
				continue
			}
			g := groupmatch.NewGrant(b.Group)
			roles = append(roles, roleCatalogEntry{
				Key:          role,
				Label:        humanizeRole(role),
				Description:  desc,
				Group:        g.Stored,
				GroupDisplay: g.Display,
				Assignable:   true,
				Scope:        "organization:" + b.Organization,
				Privileged:   privileged,
				Permissions:  perms,
			})
		}
	}
	c := roleCatalog{
		Module:      "tasks",
		DisplayName: "Tasks",
		Description: "Projects, lists, boards and tasks.",
		Roles:       roles,
	}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	c.Revision = hex.EncodeToString(sum[:8])
	return c
}

// roleCatalogHandler serves GET /api/role-catalog (role catalog contract).
// HR's backend reads it with HR_SERVICE_TOKEN; anyone signed in to the
// staff realm may read it with their usual token (signedIn runs the normal
// authentication, which answers 401 without a valid token).
func roleCatalogHandler(access *auth.AccessConfig, hrServiceToken string, signedIn func(http.Handler) http.Handler) http.HandlerFunc {
	catalog := buildRoleCatalog(access)
	etag := `"` + catalog.Revision + `"`
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
	viaSignIn := signedIn(serve)
	return func(w http.ResponseWriter, r *http.Request) {
		if hrServiceToken != "" {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(hrServiceToken)) == 1 {
				serve(w, r)
				return
			}
		}
		viaSignIn.ServeHTTP(w, r)
	}
}

func humanizeRole(role string) string {
	parts := strings.Split(strings.ReplaceAll(role, "_", " "), " ")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
