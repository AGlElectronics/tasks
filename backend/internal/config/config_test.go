package config

import (
	"strings"
	"testing"

	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
)

// testKey is a valid DEN_LINK_ENCRYPTION_KEY (32 bytes, base64); test-only.
const testKey = "dGVzdC1vbmx5LXRhc2tzLWRlbi1saW5rLWtleS0wMDA="

// clearEnv blanks every variable Load reads, so the test sees the built-in
// defaults whatever the developer's shell has set. getEnv treats "" as unset.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_ENV", "AUTH_MODE", "OIDC_ISSUER_URL", "OIDC_AUDIENCE",
		"OIDC_GROUPS_CLAIM", "OIDC_GROUP_MATCH", "OIDC_GROUP_PREFIX",
		"DATABASE_URL", "DEN_MODE", "DEN_INTERNAL_URL", "DEN_ALLOWED_HOSTS",
		"DEN_LINK_ENCRYPTION_KEY", "DEN_LINK_ENCRYPTION_KEY_PREVIOUS", "TRUSTED_PROXY_CIDRS",
		"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "SMTP_SECURITY", "TASKS_PUBLIC_URL",
		"HR_SERVICE_TOKEN", "TASKS_SERVICE_TOKEN",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/tasks")
}

func TestDefaultsFailClosed(t *testing.T) {
	clearEnv(t)
	_, err := Load()
	if err == nil {
		t.Fatal("Load with no APP_ENV/AUTH_MODE must fail until OIDC is configured")
	}
	for _, want := range []string{"OIDC_ISSUER_URL is required", "DEN_LINK_ENCRYPTION_KEY is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
}

func TestDefaultsAreProductionOIDC(t *testing.T) {
	clearEnv(t)
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.org/realms/x")
	t.Setenv("DEN_LINK_ENCRYPTION_KEY", testKey)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AppEnv != "production" || cfg.AuthMode != AuthModeOIDC {
		t.Fatalf("defaults: APP_ENV=%q AUTH_MODE=%q", cfg.AppEnv, cfg.AuthMode)
	}
	if cfg.OIDCAudience != "tasks-api" {
		t.Errorf("OIDC_AUDIENCE default = %q, want tasks-api", cfg.OIDCAudience)
	}
	if cfg.Groups.ClaimName() != "groups" || cfg.Groups.Mode != groupmatch.ModeFull || cfg.Groups.Prefix != "" {
		t.Errorf("group rule defaults: %+v", cfg.Groups)
	}
	if cfg.Den.Slug != "tasks" || cfg.Den.Mode != "" {
		t.Errorf("den defaults: slug %q mode %q", cfg.Den.Slug, cfg.Den.Mode)
	}
}

func TestSharedSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", " Development ")
	t.Setenv("AUTH_MODE", "dev")
	t.Setenv("OIDC_GROUP_MATCH", "last")
	t.Setenv("OIDC_GROUP_PREFIX", "/con2026")
	t.Setenv("OIDC_GROUPS_CLAIM", "realm_access.roles")
	t.Setenv("HR_SERVICE_TOKEN", " hr-token ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "development" || !cfg.Auth().DevAuth() {
		t.Errorf("APP_ENV is normalised: %q", cfg.AppEnv)
	}
	if cfg.Groups.Mode != groupmatch.ModeLast || cfg.Groups.Prefix != "/con2026" || cfg.Groups.Claim != "realm_access.roles" {
		t.Errorf("group rule: %+v", cfg.Groups)
	}
	if cfg.HRServiceToken != "hr-token" {
		t.Errorf("HR_SERVICE_TOKEN = %q", cfg.HRServiceToken)
	}
	t.Setenv("OIDC_GROUP_MATCH", "segment")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OIDC_GROUP_MATCH") {
		t.Errorf("unknown OIDC_GROUP_MATCH must fail, got %v", err)
	}
}

func TestSMTPSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "dev")
	t.Setenv("SMTP_HOST", "mail.example.org")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SMTP_FROM is required") {
		t.Fatalf("SMTP_HOST without SMTP_FROM must fail, got %v", err)
	}
	t.Setenv("SMTP_FROM", "tasks@example.org")
	t.Setenv("SMTP_SECURITY", "ssl")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SMTP_SECURITY") {
		t.Fatalf("unknown SMTP_SECURITY must fail, got %v", err)
	}
	t.Setenv("SMTP_SECURITY", "TLS")
	t.Setenv("TASKS_PUBLIC_URL", "https://tasks.example.org/")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.SMTP(); s.Security != "tls" || s.Port != 465 || s.FromName != "Tasks" || cfg.PublicBaseURL != "https://tasks.example.org" {
		t.Fatalf("SMTP config: %+v %q", s, cfg.PublicBaseURL)
	}
}

func TestDevModeNeedsDevelopment(t *testing.T) {
	clearEnv(t)
	t.Setenv("AUTH_MODE", "dev")
	t.Setenv("DEN_LINK_ENCRYPTION_KEY", testKey)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "only allowed with APP_ENV=development") {
		t.Fatalf("AUTH_MODE=dev without APP_ENV=development must fail, got %v", err)
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("DEN_LINK_ENCRYPTION_KEY", "")
	if _, err := Load(); err != nil {
		t.Fatalf("explicit dev setup must load (no key needed in development): %v", err)
	}
}
