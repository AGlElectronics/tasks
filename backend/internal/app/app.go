// Package app wires configuration, the database pool, auth and HTTP into a
// runnable server. cmd/server/main.go just calls Run.
//
// This package does NOT run Goose migrations on start. Migrations are a
// one-shot controlled operation handled by cmd/migrate (Compose service /
// K8s Job), same as every other Con Services module.
package app

import (
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/brand"
	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/db"
	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/files"
	httpx "github.com/AGlElectronics/tasks/backend/internal/http"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
	"github.com/AGlElectronics/tasks/backend/internal/tlscert"
)

// Version is set via -ldflags at build time; left as "dev" for local runs.
var Version = "dev"

// Run boots the server and blocks until a shutdown signal is received or
// ListenAndServe fails.
func Run(ctx context.Context, logger zerolog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger.Info().
		Str("env", cfg.AppEnv).
		Str("addr", cfg.HTTPAddr).
		Str("auth_mode", string(cfg.AuthMode)).
		Str("version", Version).
		Msg("starting tasks backend")

	// DB pool. Migrations are NOT applied here; cmd/migrate ran first.
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := db.Open(dbCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer pool.Close()
	logger.Info().Msg("database pool ready")

	// OIDC verifier. Only required when AUTH_MODE=oidc; dev mode skips.
	var oidcVerifier *auth.OIDCVerifier
	if cfg.AuthMode == config.AuthModeOIDC {
		oidcCtx, cancelOIDC := context.WithTimeout(ctx, 30*time.Second)
		oidcVerifier, err = auth.NewOIDCVerifier(oidcCtx, cfg)
		cancelOIDC()
		if err != nil {
			return fmt.Errorf("init oidc verifier: %w", err)
		}
		logger.Info().Str("issuer", cfg.OIDCIssuerURL).Msg("oidc verifier ready")
	}

	// Access config catalog. A bogus ACCESS_CONFIG_PATH fails loudly; no
	// file at all falls back to the built-in role catalog (dev only).
	var access *auth.AccessConfig
	if cfg.AccessConfigPath != "" {
		access, err = auth.LoadAccessConfig(cfg.AccessConfigPath)
		if err != nil {
			return fmt.Errorf("load access config: %w", err)
		}
		logger.Info().
			Str("path", cfg.AccessConfigPath).
			Strs("roles", access.Roles()).
			Msg("access config loaded")
	} else {
		access = auth.DefaultAccessConfig()
		logger.Info().Msg("no access config file configured; using built-in role catalog")
	}

	brandProvider := brand.New(cfg)
	den := denlink.New(pool, denlink.Config{Slug: "tasks", Version: Version, InternalURL: cfg.DenInternalURL, Mode: cfg.DenMode}, logger)
	if err := den.Restore(ctx); err != nil {
		logger.Warn().Err(err).Msg("den link: load failed")
	}
	go den.Run(ctx)

	// Attachment storage (TASK-7). Optional: nil means "not configured".
	s3Ctx, cancelS3 := context.WithTimeout(ctx, 30*time.Second)
	filesClient, err := files.New(s3Ctx, cfg)
	cancelS3()
	if err != nil {
		return fmt.Errorf("init attachment storage: %w", err)
	}
	if filesClient != nil {
		logger.Info().Str("bucket", cfg.S3Bucket).Msg("attachment storage ready")
	} else {
		logger.Info().Msg("S3 not configured; attachments disabled")
	}

	// Background jobs run in this process; Tasks runs one backend replica
	// (docs/adr/0001-single-backend-replica.md).
	jobs := tasks.NewService(pool)
	jobs.SetEnvSMTP(cfg.SMTP())
	jobs.SetPublicURL(cfg.PublicBaseURL)
	if cfg.SMTPHost != "" {
		logger.Info().Str("host", cfg.SMTPHost).Msg("own SMTP configured (Den's SMTP wins when paired and sent)")
	}
	every := func(name string, interval time.Duration, run func(context.Context) (int, error)) {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if n, err := run(ctx); err != nil {
						logger.Error().Err(err).Str("job", name).Msg("background job failed")
					} else if n > 0 {
						logger.Info().Int("count", n).Str("job", name).Msg("background job done")
					}
				}
			}
		}()
	}
	// "Due approaching" automations (TASK-22).
	every("due_automations", time.Minute, jobs.RunDueSweep)
	every("email_outbox", time.Minute, func(ctx context.Context) (int, error) {
		return jobs.RunEmailOutbox(ctx, mail.Send, tasks.EmailDelay)
	})
	every("due_reminders", 15*time.Minute, jobs.RunDueReminders)
	every("retention", time.Hour, func(ctx context.Context) (int, error) {
		res, err := jobs.RunRetention(ctx)
		if filesClient != nil {
			for _, k := range res.ObjectKeys {
				if rerr := filesClient.Remove(ctx, k); rerr != nil {
					logger.Warn().Err(rerr).Str("key", k).Msg("retention: could not remove attachment object")
				}
			}
		}
		if res.Tasks+res.Notifications+res.Audit+res.AutomationRuns > 0 {
			logger.Info().Int("tasks", res.Tasks).Int("notifications", res.Notifications).
				Int("audit", res.Audit).Int("automation_runs", res.AutomationRuns).Msg("retention applied")
		}
		return 0, err
	})

	router := httpx.NewRouter(httpx.Deps{
		Config:       cfg,
		Pool:         pool,
		Logger:       logger,
		Brand:        brandProvider,
		Den:          den,
		OIDCVerifier: oidcVerifier,
		Access:       access,
		Files:        filesClient,
	}, Version)

	server := &nethttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if _, err := tlscert.Configure(ctx, server, logger); err != nil {
		return fmt.Errorf("tls: %w", err)
	}

	return runServer(ctx, server, pool, cfg.ShutdownTimeout, logger)
}

func runServer(ctx context.Context, server *nethttp.Server, pool *pgxpool.Pool, shutdownTimeout time.Duration, logger zerolog.Logger) error {
	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", server.Addr).Msg("http listening")
		if err := tlscert.ListenAndServe(server); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-signalCtx.Done():
		logger.Info().Msg("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}
	pool.Close()
	logger.Info().Msg("shutdown complete")
	return nil
}
