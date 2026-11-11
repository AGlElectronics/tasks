// Package config loads and validates the backend runtime configuration from
// environment variables (12-factor). Validation runs once at startup and
// fails loudly: a misconfigured deployment is better than a silently
// degraded one.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/AGlElectronics/tasks/backend/internal/authbase"
	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
)

// AuthMode selects how incoming requests are authenticated.
//
//   - AuthModeDev reads an X-Dev-User header (identity) and an X-Dev-Groups
//     header (simulated IdP groups) and provisions a local user tied to the
//     "dev" issuer. Only allowed with APP_ENV=development (authbase).
//   - AuthModeOIDC verifies a Bearer access token via the configured issuer.
type AuthMode = authbase.AuthMode

const (
	AuthModeOIDC = authbase.AuthOIDC
	AuthModeDev  = authbase.AuthDev
)

// DefaultOIDCAudience is the audience of the access tokens Keycloak issues
// for the Tasks API (the token contract's audience mapper on tasks-spa).
const DefaultOIDCAudience = "tasks-api"

// Config is the validated process configuration.
type Config struct {
	// Core
	AppEnv     string
	AppBaseURL string
	HTTPAddr   string

	// Database
	DatabaseURL string

	// Auth
	AuthMode      AuthMode
	OIDCIssuerURL string
	// OIDCAudience is the aud an access token must carry (OIDC_AUDIENCE,
	// default tasks-api).
	OIDCAudience     string
	AccessConfigPath string
	DevDefaultUser   string
	DevDefaultGroups []string
	// Groups is the shared group matching rule (OIDC_GROUPS_CLAIM,
	// OIDC_GROUP_MATCH, OIDC_GROUP_PREFIX).
	Groups groupmatch.Config

	// TasksServiceToken protects /api/internal/* (service-to-service calls
	// from HR). Empty disables those routes.
	TasksServiceToken string
	// HRServiceToken lets HR read GET /api/role-catalog without a sign-in.
	// Empty: only signed-in staff can read it.
	HRServiceToken string

	// S3 (attachments, TASK-7). Optional: with S3_ENDPOINT unset the API
	// answers "attachments are not configured" and everything else works.
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Region          string
	S3UsePathStyle    bool

	// SMTP for email notifications when Den does not send its own (the
	// module is standalone, or Den's bundle has no smtp). Empty host: no
	// email unless Den provides it.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string
	// SMTPSecurity is starttls (default), tls (implicit TLS, port 465) or
	// none (plain, only for a local test server such as Mailpit).
	SMTPSecurity string
	// PublicBaseURL is the web app's address, used for links in emails
	// (TASKS_PUBLIC_URL). Empty: emails carry the task key without a link.
	PublicBaseURL string

	// Brand provider
	BrandLocalPath string // optional brand.json; Den's brand overlays it once paired
	// Den is the pairing configuration (DEN_MODE, DEN_INTERNAL_URL,
	// DEN_ALLOWED_HOSTS, DEN_LINK_ENCRYPTION_KEY…). Den.Version is set by
	// the app at start-up.
	Den denlink.Config

	// HTTP
	CORSAllowedOrigins []string
	ShutdownTimeout    time.Duration

	// auth is the shared AUTH_MODE/APP_ENV guard, including the trusted
	// proxies from TRUSTED_PROXY_CIDRS. Tests that build a Config by hand
	// leave it zero; Auth fills AppEnv and AuthMode from the fields above.
	auth authbase.Config
}

// Load reads environment variables (plus an optional .env file) and returns
// a validated Config. Required fields that are missing return an aggregated
// error so operators see every problem at once instead of fixing one at a
// time.
func Load() (*Config, error) {
	_ = godotenv.Load()

	// The shared packages read their own settings; validate() reports
	// their errors together with ours.
	// Fail closed: a deployment that sets neither APP_ENV nor AUTH_MODE
	// runs as production with OIDC, never with trusted dev headers.
	ab, abErr := authbase.FromEnv(os.Getenv)
	groups, _ := groupmatch.FromEnv(os.Getenv)
	den, _ := denlink.FromEnv("tasks", "", os.Getenv)

	cfg := &Config{
		AppEnv: ab.AppEnv,
		// Container-internal defaults: every module in the ecosystem binds
		// :8080 inside its container. Host-side port mapping (host 8160 ->
		// container 8080 for Tasks) is handled by docker-compose / K8s.
		AppBaseURL: getEnv("APP_BASE_URL", "http://localhost:8080"),
		HTTPAddr:   getEnv("HTTP_ADDR", ":8080"),

		DatabaseURL: getEnv("DATABASE_URL", ""),

		AuthMode:         ab.AuthMode,
		OIDCIssuerURL:    getEnv("OIDC_ISSUER_URL", ""),
		OIDCAudience:     getEnv("OIDC_AUDIENCE", DefaultOIDCAudience),
		AccessConfigPath: getEnv("ACCESS_CONFIG_PATH", ""),
		DevDefaultUser:   getEnv("DEV_DEFAULT_USER", "dev-admin"),
		DevDefaultGroups: splitCSV(getEnv("DEV_DEFAULT_GROUPS", "tasks-admin")),
		Groups:           groups,

		TasksServiceToken: strings.TrimSpace(getEnv("TASKS_SERVICE_TOKEN", "")),
		HRServiceToken:    strings.TrimSpace(getEnv("HR_SERVICE_TOKEN", "")),

		S3Endpoint:        getEnv("S3_ENDPOINT", ""),
		S3Bucket:          getEnv("S3_BUCKET", "tasks-data-dev"),
		S3AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", ""),
		S3Region:          getEnv("S3_REGION", "us-east-1"),
		S3UsePathStyle:    getEnvBool("S3_USE_PATH_STYLE", true),

		SMTPHost:      getEnv("SMTP_HOST", ""),
		SMTPPort:      getEnvInt("SMTP_PORT", 0),
		SMTPUsername:  getEnv("SMTP_USERNAME", ""),
		SMTPPassword:  getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:      getEnv("SMTP_FROM", ""),
		SMTPFromName:  getEnv("SMTP_FROM_NAME", "Tasks"),
		SMTPSecurity:  strings.ToLower(getEnv("SMTP_SECURITY", "starttls")),
		PublicBaseURL: strings.TrimRight(getEnv("TASKS_PUBLIC_URL", ""), "/"),

		BrandLocalPath: getEnv("BRAND_LOCAL_PATH", ""),
		Den:            den,

		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3009")),
		ShutdownTimeout:    getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		auth:               ab,
	}

	if err := cfg.validate(abErr); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate(authErr error) error {
	var errs []string

	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	if err := c.Den.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := c.Groups.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	// FromEnv checks AUTH_MODE and TRUSTED_PROXY_CIDRS. Report a bad proxy
	// list as well as a bad mode, without repeating the same sentence.
	if authErr != nil {
		if v := c.Auth().Validate(); v == nil || v.Error() != authErr.Error() {
			errs = append(errs, authErr.Error())
		}
	}
	if err := c.Auth().Validate(); err != nil {
		errs = append(errs, err.Error())
	} else if c.AuthMode != AuthModeDev {
		if c.OIDCIssuerURL == "" {
			errs = append(errs, "OIDC_ISSUER_URL is required when AUTH_MODE=oidc")
		}
		if c.OIDCAudience == "" {
			errs = append(errs, "OIDC_AUDIENCE must not be empty when AUTH_MODE=oidc")
		}
	}

	switch c.SMTPSecurity {
	case "starttls", "tls", "none":
	default:
		errs = append(errs, fmt.Sprintf("SMTP_SECURITY must be starttls, tls or none, got %q", c.SMTPSecurity))
	}
	if c.SMTPHost != "" && c.SMTPFrom == "" {
		errs = append(errs, "SMTP_FROM is required when SMTP_HOST is set")
	}

	if len(errs) > 0 {
		return errors.New("invalid config: " + strings.Join(errs, "; "))
	}
	return nil
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return parsed
}

// Auth is the shared AUTH_MODE/APP_ENV guard for this configuration,
// including the trusted proxies loaded from TRUSTED_PROXY_CIDRS.
func (c *Config) Auth() authbase.Config {
	a := c.auth
	if c.AppEnv != "" {
		a.AppEnv = authbase.NormalizeAppEnv(c.AppEnv)
	}
	if c.AuthMode != "" {
		a.AuthMode = c.AuthMode
	}
	return a
}

// SMTP is Tasks' own SMTP settings (used when Den sends none).
func (c *Config) SMTP() mail.Config {
	return mail.Config{
		Host: c.SMTPHost, Port: c.SMTPPort, Username: c.SMTPUsername, Password: c.SMTPPassword,
		From: c.SMTPFrom, FromName: c.SMTPFromName, Security: c.SMTPSecurity,
	}.Normalized()
}

// S3Enabled reports whether attachment storage is configured.
func (c *Config) S3Enabled() bool { return c.S3Endpoint != "" }

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
