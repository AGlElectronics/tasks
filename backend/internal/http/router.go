// Package http wires the chi router and ties handlers to their
// dependencies. The structure deliberately mirrors the other Con Services
// modules: one Deps struct consumed by one NewRouter, with per-resource
// handler types owning their own routing closure.
//
// API shape:
//
//	GET /livez             — process liveness (unauthenticated)
//	GET /readyz            — service readiness (unauthenticated)
//	GET /api/health        — richer status for humans
//	GET /api/role-catalog  — public role catalog for HR's access picker
//	POST /api/internal/departments — HR department→space sync (TASKS_SERVICE_TOKEN)
//	GET /api/v1/brand      — active brand package (unauthenticated)
//	GET /api/v1/me         — authenticated principal + permissions
//	GET|POST /api/v1/spaces, GET|PATCH|DELETE /api/v1/spaces/{space}
//	PUT /api/v1/spaces/{space}/statuses, POST /api/v1/spaces/{space}/lists
//	GET|PATCH|DELETE /api/v1/lists/{list}, GET /api/v1/lists/{list}/tasks
//	GET /api/v1/my-work, POST /api/v1/tasks
//	GET|PATCH|DELETE /api/v1/tasks/{task} — {task} is a uuid or KEY-N
//	POST /api/v1/tasks/{task}/comments, DELETE /api/v1/comments/{comment}
//	GET /api/v1/users     — people picker
//	/api/v1/den…           — Den pairing admin (settings.manage)
//
// Everything under /api/v1 (except /brand) requires authentication; the
// auth middleware is applied inside a chi.Group so the shape is explicit.
package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/brand"
	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/files"
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
}

// NewRouter builds the chi router with middleware, CORS, and routes.
// Version is passed in from cmd/server at build time.
func NewRouter(deps Deps, version string) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))
	// Cap request bodies at 1 MB (TASK-39); attachment uploads (TASK-7)
	// are multipart and get their own, larger cap.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Body != nil {
				limit := int64(1 << 20)
				if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/attachments") {
					limit = maxAttachmentSize + (1 << 20)
				}
				req.Body = http.MaxBytesReader(w, req.Body, limit)
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

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Dev-User", "X-Dev-Groups", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID", "X-Next-Cursor"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	health := newHealthHandler(deps.Pool, version)
	resolver := auth.NewResolver(deps.Pool, deps.Access)
	authMW := auth.Middleware(deps.Config, resolver, deps.OIDCVerifier)

	den := deps.Den
	if den == nil {
		den = denlink.New(deps.Pool, denlink.Config{Slug: "tasks", Version: version}, deps.Logger)
	}
	brandH := &brandHandler{provider: deps.Brand, den: den}
	th := &taskHandler{svc: tasks.NewService(deps.Pool), files: deps.Files}

	// Orchestrator probes — unauthenticated, served from the root so they
	// don't collide with any /api/* middleware.
	r.Get("/livez", health.livez)
	r.Get("/readyz", health.readyz)

	r.Route("/api", func(api chi.Router) {
		api.Get("/health", health.get)
		api.Get("/role-catalog", roleCatalogHandler(deps.Access))
		// Service-to-service: HR ensures one space per department
		// (TASK-25). Guarded by TASKS_SERVICE_TOKEN, not user auth.
		api.Post("/internal/departments", internalDepartmentsHandler(deps.Config, th.svc))

		api.Route("/v1", func(v1 chi.Router) {
			// Brand is public so the login screen can render before the
			// user authenticates.
			v1.Get("/brand", brandH.get)
			v1.Get("/brand/{role}", den.AssetHandler)

			v1.Group(func(secure chi.Router) {
				secure.Use(authMW)
				// Per-user token bucket (TASK-41): ~20 req/s sustained
				// with room for bursty page loads.
				secure.Use(newRateLimiter(20, 100).middleware)
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
				secure.Post("/spaces/{space}/folders", handle(th.createFolder))
				secure.Patch("/folders/{folder}", handle(th.updateFolder))
				secure.Delete("/folders/{folder}", handle(th.deleteFolder))
				secure.Get("/spaces/{space}/archived-lists", handle(th.archivedLists))
				secure.Get("/spaces/{space}/trash", handle(th.trash))
				secure.Post("/tasks/{task}/restore", handle(th.restoreTask))
				secure.Delete("/tasks/{task}/purge", handle(th.purgeTask))

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
