package config

import (
	"strings"
	"testing"
)

// clearEnv blanks every variable Load reads, so the test sees the built-in
// defaults whatever the developer's shell has set. getEnv treats "" as unset.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_ENV", "AUTH_MODE", "OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_AUDIENCE",
		"DATABASE_URL", "DEN_MODE", "TRUSTED_PROXY_CIDRS",
		"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "SMTP_SECURITY", "TASKS_PUBLIC_URL",
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
	if !strings.Contains(err.Error(), "OIDC_ISSUER_URL is required") {
		t.Fatalf("want the OIDC requirement, got %v", err)
	}
}

func TestDefaultsAreProductionOIDC(t *testing.T) {
	clearEnv(t)
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.org/realms/x")
	t.Setenv("OIDC_CLIENT_ID", "tasks-spa")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AppEnv != "production" || cfg.AuthMode != AuthModeOIDC {
		t.Fatalf("defaults: APP_ENV=%q AUTH_MODE=%q", cfg.AppEnv, cfg.AuthMode)
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
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "only allowed when APP_ENV=development") {
		t.Fatalf("AUTH_MODE=dev without APP_ENV=development must fail, got %v", err)
	}
	t.Setenv("APP_ENV", "development")
	if _, err := Load(); err != nil {
		t.Fatalf("explicit dev setup must load: %v", err)
	}
}
