package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
)

// OIDCVerifier wraps the go-oidc verifier and exposes the subset of token
// claims Tasks cares about. The verifier handles JWKS rotation via OIDC
// discovery; nothing identity-provider-specific lives in this code.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
	issuer   string
	groups   groupmatch.Config
}

// OIDCClaims is the parsed claim subset Tasks stores.
type OIDCClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Nickname          string `json:"nickname"`
	// Groups come only from the claim named by OIDC_GROUPS_CLAIM
	// (groupmatch); the roles claim is never merged in.
	Groups []string `json:"-"`
}

// NewOIDCVerifier runs OIDC discovery and returns a verifier for access
// tokens whose aud contains OIDC_AUDIENCE (tasks-api by default; see the
// token contract). Call once at startup; the returned object is safe for
// concurrent use.
func NewOIDCVerifier(ctx context.Context, cfg *config.Config) (*OIDCVerifier, error) {
	if cfg.OIDCIssuerURL == "" || cfg.OIDCAudience == "" {
		return nil, errors.New("OIDC verifier requires OIDC_ISSUER_URL and OIDC_AUDIENCE")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &OIDCVerifier{
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDCAudience}),
		issuer:   cfg.OIDCIssuerURL,
		groups:   cfg.Groups,
	}, nil
}

// Verify parses and validates a bearer token. On success the issuer claim
// is populated from the verifier config so downstream code never has to
// trust a client-sent value.
func (v *OIDCVerifier) Verify(ctx context.Context, bearer string) (*OIDCClaims, error) {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return nil, errors.New("missing bearer token")
	}
	token, err := v.verifier.Verify(ctx, bearer)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var c OIDCClaims
	if err := token.Claims(&c); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if c.Subject == "" {
		return nil, errors.New("token missing sub claim")
	}
	var raw map[string]any
	if err := token.Claims(&raw); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	c.Groups = v.groups.Groups(raw)
	// Pin the issuer to what the verifier was configured with. Keycloak
	// token issuers include trailing /realm paths; always use what we
	// trust, not what the token asserts.
	c.Issuer = v.issuer
	return &c, nil
}
