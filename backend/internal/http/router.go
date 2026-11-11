// Package http wires the chi router and ties handlers to their
// dependencies. The structure deliberately mirrors the other Con Services
// modules: one Deps struct consumed by one NewRouter, with per-resource
// handler types owning their own routing closure.
//
// API shape (the full reference is docs/modules/tasks/api.md in
// con-services; the routes below are the source of truth):
//
//	GET /livez, /readyz, /api/health — probes and status (unauthenticated)
//	GET /api/den/state, /api/v1/brand — public
//	GET /api/role-catalog — HR (HR_SERVICE_TOKEN) or any signed-in person
//	GET /api/portal/todos — Den's backend, with the pairing credential
//	POST /api/internal/departments — HR department hook (TASKS_SERVICE_TOKEN)
//	/api/v1/me, /me/preferences, /me/notifications — the signed-in person
//	/api/v1/spaces…, /lists…, /tasks… — spaces, statuses, lists, grants,
//	    tasks ({task} is a uuid or KEY-N), comments, events, checklists,
//	    watchers, attachments, tags, folders, custom fields, dependencies,
//	    time, automations, trash, import
//	GET /api/v1/my-work, /inbox…, /search, /calendar, /favorites, /users
//	GET /api/v1/stream — live updates (Server-Sent Events)
//	/api/v1/audit, /settings… , /den… — admin (settings.manage)
//
// Everything under /api/v1 (except /brand) requires authentication; the
// auth middleware is applied inside a chi.Group so the shape is explicit.
// With DEN_MODE=required, the /api routes other than health, role catalog,
// brand, me and den answer 503 den_pairing_required until Den pairs.
package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/authbase"
	"github.com/AGlElectronics/tasks/backend/internal/brand"
	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/files"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
	"github.com/AGlElectronics/tasks/backend/internal/realtime"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// Deps groups the runtime dependencies the HTTP layer needs.
type Deps struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Logger       zerolog.Logger
	Brand        brand.Provider
	OIDCVerifier *auth.OIDCVerifier
	Access       *auth.AccessConfig
	// Den is the pairing with Den; nil means a fresh standalone client.
	Den *denlink.Client
	// Files is attachment storage; nil means attachments are unavailable.
	Files *files.Client
	// SendMail delivers email; nil means mail.Send (tests record instead).
	SendMail tasks.SendFunc
	// Hub feeds live updates (GET /api/v1/stream); nil turns them off.
	Hub *realtime.Hub
}

// streamPath is exempt from the request timeout: it stays open.
const streamPath = "/api/v1/stream"

// NewRouter builds the chi router with middleware, CORS, and routes.
// Version is passed in from cmd/server at build time.
func NewRouter(deps Deps, version string) http.Handler {
	r := chi.NewRouter()
	authCfg := deps.Config.Auth()

	// RemoteAddr becomes the real client before logs and the rate limiter.
	// The zero value (tests that build a Config by hand) trusts no proxy.
	r.Use(authCfg.TrustedProxies.Middleware)
	r.Use(authCfg.StripDevHeaders)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	timeout := chimiddleware.Timeout(60 * time.Second)
	r.Use(func(next http.Handler) http.Handler {
		limited := timeout(next)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == streamPath {
				next.ServeHTTP(w, req)
				return
			}
			limited.ServeHTTP(w, req)
		})
	})
	// Cap request bodies (TASK-39); see bodyLimit. JSON bodies are decoded
	// with authbase.DecodeJSON under the same cap.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Body != nil {
				req.Body = http.MaxBytesReader(w, req.Body, bodyLimit(req))
			}
			next.ServeHTTP(w, req)
		})
	})

	r.Use(hlog.NewHandler(deps.Logger))
	r.Use(hlog.AccessHandler(func(req *http.Request, status, size int, duration time.Duration) {
		hlog.FromRequest(req).Info().
			Str("method", req.Method).
			Str("path", req.URL.Path).
			Int("status", status).
			Int("size", size).
			Dur("duration", duration).
			Msg("http")
	}))
	r.Use(hlog.RequestIDHandler("request_id", "X-Request-ID"))

	// X-Request-ID is exposed by default; the web app also reads X-Next-Cursor.
	// X-Dev-User and X-Dev-Groups are allowed only in dev mode.
	r.Use(authCfg.CORS(authbase.CORSOptions{
		AllowedOrigins: deps.Config.CORSAllowedOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		ExposeHeaders:  []string{"X-Request-ID", "X-Next-Cursor"},
	}))

	health := newHealthHandler(deps.Pool, version)
	resolver := auth.NewResolver(deps.Pool, deps.Access, deps.Config.Groups)
	authMW := auth.Middleware(deps.Config, resolver, deps.OIDCVerifier)

	den := deps.Den
	if den == nil {
		dc := deps.Config.Den
		dc.Slug, dc.Version = "tasks", version
		den = denlink.New(deps.Pool, dc, deps.Logger)
	}
	brandH := &brandHandler{provider: deps.Brand, den: den}
	svc := tasks.NewService(deps.Pool)
	svc.SetEnvSMTP(deps.Config.SMTP())
	svc.SetPublicURL(deps.Config.PublicBaseURL)
	svc.SetGroupMatch(deps.Config.Groups)
	svc.SetDen(den)
	send := deps.SendMail
	if send == nil {
		send = mail.Send
	}
	th := &taskHandler{svc: svc, files: deps.Files, send: send, hub: deps.Hub}

	// Orchestrator probes — unauthenticated, served from the root so they
	// don't collide with any /api/* middleware.
	r.Get("/livez", health.livez)
	r.Get("/readyz", health.readyz)

	// Per-person token bucket (TASK-41): ~20 req/s sustained with room for
	// bursty page loads. It exists to stop a runaway script, not real use.
	limiter := authbase.NewRateLimiter(authbase.RateLimitOptions{
		Rate: 20, Burst: 100,
		Key: authbase.BySubject(func(r *http.Request) (authbase.UserKey, bool) {
			u, ok := auth.FromContext(r.Context())
			if !ok {
				return authbase.UserKey{}, false
			}
			return authbase.UserKey{Issuer: u.Issuer, Subject: u.Subject}, true
		}),
	})

	r.Route("/api", func(api chi.Router) {
		api.Use(authbase.SecurityHeaders)
		api.Use(den.RequirePaired)
		api.Get("/health", health.get)
		api.Get("/role-catalog", roleCatalogHandler(deps.Access, deps.Config.HRServiceToken, authMW))
		api.Get("/portal/todos", portalTodos(den, svc))
		api.Get("/den/state", den.StateHandler)
		// Service-to-service: HR ensures one space per department
		// (TASK-25). Guarded by TASKS_SERVICE_TOKEN, not user auth.
		api.Post("/internal/departments", internalDepartmentsHandler(deps.Config, th.svc))

		api.Route("/v1", func(v1 chi.Router) {
			// Brand is public so the login screen can render before the
			// user authenticates.
			v1.Get("/brand", brandH.get)
			v1.Get("/brand/{role}", den.AssetHandler)
			v1.Get("/den/state", den.StateHandler)

			v1.Group(func(secure chi.Router) {
				secure.Use(authMW)
				secure.Use(limiter.Middleware)
				secure.Get("/me", meHandler)
				prefs := den.PreferencesHandler(func(r *http.Request) (string, string, bool) {
					u, ok := auth.FromContext(r.Context())
					if !ok || u.Issuer == "" || u.Subject == "" {
						return "", "", false
					}
					return u.Issuer, u.Subject, true
				})
				secure.Get("/me/preferences", prefs)
				secure.Put("/me/preferences", prefs)
				secure.Route("/den", func(dr chi.Router) {
					dr.Use(requirePermission(domain.PermSettingsManage))
					dr.Get("/", den.StatusHandler)
					dr.Post("/pair", den.PairHandler)
					dr.Post("/sync", den.SyncHandler)
					dr.Delete("/pair", den.UnpairHandler)
				})

				secure.Group(func(admin chi.Router) {
					admin.Use(requirePermission(domain.PermSettingsManage))
					admin.Get("/audit", handle(th.audit))
					admin.Get("/settings", handle(th.getSettings))
					admin.Put("/settings", handle(th.putSettings))
					admin.Post("/settings/email/test", handle(th.testEmail))
				})
				secure.Get("/stream", th.stream)
				secure.Get("/stream/status", handle(th.streamStatus))
				secure.Get("/me/notifications", handle(th.notificationPrefs))
				secure.Put("/me/notifications", handle(th.notificationPrefs))

				secure.Get("/calendar", handle(th.calendar))
				secure.Get("/users", handle(th.people))
				secure.Get("/my-work", handle(th.myWork))

				secure.Get("/spaces", handle(th.listSpaces))
				secure.Post("/spaces", handle(th.createSpace))
				secure.Get("/spaces/{space}", handle(th.getSpace))
				secure.Patch("/spaces/{space}", handle(th.updateSpace))
				secure.Delete("/spaces/{space}", handle(th.deleteSpace))
				secure.Put("/spaces/{space}/statuses", handle(th.replaceStatuses))
				secure.Post("/spaces/{space}/lists", handle(th.createList))
				secure.Get("/spaces/{space}/grants", handle(th.listGrants))
				secure.Put("/spaces/{space}/grants", handle(th.putGrant))
				secure.Delete("/spaces/{space}/grants/{grant}", handle(th.deleteGrant))

				secure.Get("/lists/{list}", handle(th.getList))
				secure.Patch("/lists/{list}", handle(th.updateList))
				secure.Delete("/lists/{list}", handle(th.deleteList))
				secure.Get("/lists/{list}/tasks", handle(th.listTasks))
				secure.Get("/lists/{list}/grants", handle(th.listGrants))
				secure.Put("/lists/{list}/grants", handle(th.putGrant))
				secure.Delete("/lists/{list}/grants/{grant}", handle(th.deleteGrant))

				secure.Post("/tasks", handle(th.createTask))
				secure.Get("/tasks/{task}", handle(th.getTask))
				secure.Patch("/tasks/{task}", handle(th.updateTask))
				secure.Delete("/tasks/{task}", handle(th.deleteTask))
				secure.Get("/tasks/{task}/comments", handle(th.taskComments))
				secure.Get("/tasks/{task}/events", handle(th.taskEvents))
				secure.Post("/tasks/{task}/comments", handle(th.addComment))
				secure.Delete("/comments/{comment}", handle(th.deleteComment))
				secure.Patch("/comments/{comment}", handle(th.updateComment))

				// Phase-4 features
				secure.Get("/search", handle(th.search))
				secure.Get("/inbox", handle(th.inbox))
				secure.Get("/inbox/unread-count", handle(th.unreadCount))
				secure.Post("/inbox/read", handle(th.markRead))
				secure.Get("/favorites", handle(th.listFavorites))
				secure.Put("/favorites", handle(th.addFavorite))
				secure.Post("/favorites/remove", handle(th.removeFavorite))

				secure.Get("/spaces/{space}/tags", handle(th.listTags))
				secure.Post("/spaces/{space}/tags", handle(th.createTag))
				secure.Patch("/spaces/{space}/tags/{tag}", handle(th.updateTag))
				secure.Delete("/spaces/{space}/tags/{tag}", handle(th.deleteTag))
				secure.Get("/spaces/{space}/fields", handle(th.listFields))
				secure.Post("/spaces/{space}/fields", handle(th.createField))
				secure.Patch("/fields/{field}", handle(th.updateField))
				secure.Delete("/fields/{field}", handle(th.deleteField))
				secure.Post("/spaces/{space}/folders", handle(th.createFolder))
				secure.Patch("/folders/{folder}", handle(th.updateFolder))
				secure.Delete("/folders/{folder}", handle(th.deleteFolder))
				secure.Get("/spaces/{space}/automations", handle(th.listAutomations))
				secure.Post("/spaces/{space}/automations", handle(th.createAutomation))
				secure.Patch("/automations/{automation}", handle(th.updateAutomation))
				secure.Delete("/automations/{automation}", handle(th.deleteAutomation))
				secure.Get("/automations/{automation}/runs", handle(th.automationRuns))
				secure.Get("/spaces/{space}/archived-lists", handle(th.archivedLists))
				secure.Get("/spaces/{space}/trash", handle(th.trash))
				secure.Post("/tasks/{task}/restore", handle(th.restoreTask))
				secure.Delete("/tasks/{task}/purge", handle(th.purgeTask))

				secure.Get("/tasks/{task}/time", handle(th.listTime))
				secure.Post("/tasks/{task}/time", handle(th.logTime))
				secure.Delete("/time/{entry}", handle(th.deleteTime))
				secure.Get("/lists/{list}/time-report", handle(th.listTimeReport))
				secure.Post("/lists/{list}/import", handle(th.importTasks))
				secure.Post("/tasks/{task}/dependencies", handle(th.addDependency))
				secure.Delete("/tasks/{task}/dependencies/{other}", handle(th.removeDependency))
				secure.Post("/tasks/{task}/watch", handle(th.watch))
				secure.Delete("/tasks/{task}/watch", handle(th.unwatch))
				secure.Post("/tasks/{task}/checklist", handle(th.addChecklistItem))
				secure.Patch("/checklist/{item}", handle(th.updateChecklistItem))
				secure.Delete("/checklist/{item}", handle(th.deleteChecklistItem))

				secure.Post("/tasks/{task}/attachments", handle(th.uploadAttachment))
				secure.Get("/attachments/{attachment}", th.downloadAttachment)
				secure.Delete("/attachments/{attachment}", handle(th.deleteAttachment))
			})
		})
	})

	return r
}
