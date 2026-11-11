package denlink

// Helpers Tasks needs that the shared denlink package does not export.
// This file has no "Code generated" header, so scripts/sync-shared leaves
// it in place. If Open, load or send change shape, these break at compile
// time.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"strings"
)

// CredentialMatches reports whether raw is the pairing credential Den
// issued to this module. A missing link, a revoked link, or a credential
// that cannot be decrypted does not match. The comparison is constant-time
// for equal lengths.
func (c *Client) CredentialMatches(ctx context.Context, raw string) bool {
	if c == nil || c.pool == nil {
		return false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	l, err := c.load(ctx)
	if err != nil || l == nil || l.credential == "" || l.revokedAt != nil {
		return false
	}
	a, b := []byte(raw), []byte(l.credential)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// SMTPPassword fetches smtp.password from Den's module-settings secrets.
// The password is not part of the bundle. An empty password with a nil
// error means Den holds none.
func (c *Client) SMTPPassword(ctx context.Context) (string, error) {
	if c == nil || c.pool == nil {
		return "", ErrNotPaired
	}
	l, err := c.load(ctx)
	if err != nil {
		return "", err
	}
	if l == nil || l.credential == "" {
		return "", ErrNotPaired
	}
	res, err := c.get(ctx, l, "/api/v1/integrations/module-settings/secrets")
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		SMTP struct {
			Password string `json:"password"`
		} `json:"smtp"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body); err != nil {
		return "", err
	}
	return body.SMTP.Password, nil
}
