# Changelog

All notable changes to Tasks are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

Released versions are tagged `MAJOR.MINOR.PATCH`; the images
`ghcr.io/aglelectronics/tasks-backend`, `tasks-frontend` and `tasks-migrate`
are published as `MAJOR.MINOR.PATCH-BUILD`, where `BUILD` counts rebuilds of
the same version. Rebuilds get no entry here.

### Added

- `TRUSTED_PROXY_CIDRS` (optional): comma-separated CIDRs of the proxies in
  front of the backend whose `X-Forwarded-For` entries are believed. The
  default, `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,::1/128,fc00::/7`,
  covers the Gateway, nginx and Caddy; change it only for a proxy outside
  the private ranges, or set `none` when no proxy is in front. See
  [Client IP addresses](https://aglelectronics.github.io/con-services/operations/production/#client-ip-addresses).
- CI on every pull request (tests, lint, Trivy scans) and signed images with an SBOM published to GHCR on version tags (linux/amd64; linux/arm64 optional).
- The backend can serve HTTPS. Set `TLS_CERT_FILE` and `TLS_KEY_FILE` to a
  PEM certificate and private key; the backend re-reads both every minute,
  so a certificate renewed by cert-manager is used without a restart. With
  neither set it serves plain HTTP as before, which is what Docker Compose
  development uses. See
  [Internal TLS](https://aglelectronics.github.io/con-services/operations/internal-tls/).
- The web app reads its installation settings (sign-in mode, issuer, client
  id, scopes, API address and product name) from `/config.json` when it starts, so
  the published `tasks-frontend` image works for every installation. nginx serves the
  file from `/etc/conservices/config.json` with `Cache-Control: no-store`,
  so a change shows on the next page load. Without a mounted file the web app
  shows "Not configured yet"; a broken file gives a clear error page. See
  [Web app settings](https://aglelectronics.github.io/con-services/modules/tasks/configuration/#web-app-settings-configjson).
- `infra/docker-compose.yml` mounts `infra/frontend-config/compose.json`
  (dev sign-in) as the web app's `/config.json`. Set `FRONTEND_CONFIG` in
  `.env` to use another file, for example one with real sign-in.

### Changed

- The web app's built-in nginx configuration (`nginx.conf`, and
  `nginx.stack.conf` for the full stack on Docker) no longer limits sign-in
  connections to `auth.conservices.test`; the Content-Security-Policy now
  allows HTTPS connections to any host (`connect-src 'self' https:`), so
  sign-in works with the Keycloak address set in `config.json`. Scripts,
  frames and other content stay limited to the web app itself.
- Kubernetes: internal TLS is on by default in the Helm chart
  (`deploy/charts/tasks`). cert-manager issues `tasks-backend-tls` and
  `tasks-frontend-tls`; the backend and the web app serve HTTPS inside the
  cluster, the web app checks the backend's certificate, and the backend
  trusts the platform's internal CA
  (`SSL_CERT_DIR=/etc/conservices-internal-ca`). The cluster needs
  cert-manager, trust-manager and the platform's `conservices-internal-ca`
  issuer. New chart values: `internalTLS.enabled`,
  `internalTLS.createCertificates`, `internalTLS.issuerRef`,
  `internalTLS.duration`, `internalTLS.renewBefore`,
  `internalTLS.caConfigMap` and `internalTLS.ingressNginxAnnotations`. A
  cluster without cert-manager sets `internalTLS.enabled: false`. See
  [Set up Tasks](https://aglelectronics.github.io/con-services/modules/tasks/setup/).
- Kubernetes: in-cluster addresses use `https://`: Den
  (`DEN_INTERNAL_URL`), MinIO (`S3_ENDPOINT`) and the department hook HR
  calls. The database URL Den shows now ends in
  `?sslmode=verify-full&sslrootcert=/etc/conservices-internal-ca/ca.crt`;
  the platform's PostgreSQL refuses connections without TLS.
- Kubernetes: the web app's Service listens on port 443 (port name
  `https`); the backend's Service stays on port 8080, now named `https`.
- **BREAKING:** The web app image (`tasks-frontend`) now runs nginx as an unprivileged user (UID 101) and listens on port **8080** inside the container instead of 80. If you run it with Docker Compose or behind your own proxy, change the target from `tasks-frontend:80` to `tasks-frontend:8080`; published host ports stay the same. The Helm chart and
  the Kubernetes manifests already use port 8080 and need no change.
- The backend is built with Go 1.27.3 (was Go 1.25). Building from source
  needs Go 1.27.3 or newer; the `go` command downloads it automatically.
- **BREAKING:** `infra/Dockerfile.frontend` no longer takes `VITE_AUTH_MODE`, `VITE_OIDC_*` and `VITE_PRODUCT_NAME`
  build arguments. If you built your own frontend image to get your sign-in
  address in, use the published image instead and put the same values in its
  `/config.json` (Helm: `frontend.config`, filled in from
  `config.OIDC_ISSUER_URL` and `config.OIDC_CLIENT_ID` when left out). The
  `VITE_*` variables now only configure the dev server (`npm run dev`).
- Form validation in the web app uses zod 4, `@hookform/resolvers` 7 and
  react-hook-form 7.55 or newer.

### Security

- Rate limits can no longer be bypassed by sending a fake `X-Forwarded-For`
  header. The backend now takes the client address from `X-Forwarded-For`
  only when the connection comes from a trusted proxy (`TRUSTED_PROXY_CIDRS`),
  and never reads `X-Real-IP` or `True-Client-IP`.
- All images are rebuilt with Go 1.27.3 and updated Go libraries
  (`go-chi/chi/v5` 5.5.0, `go-jose/v4` 4.1.9, `klauspost/compress` 1.20.3, `golang.org/x/crypto` 0.58.0, `golang.org/x/net` 0.62.0 and `golang.org/x/text` 0.43.0), fixing known vulnerabilities in
  the Go standard library and in these libraries. The CI image scan (Trivy,
  HIGH and CRITICAL issues that have a fix) passes for every image.
- The web app image is built on `nginxinc/nginx-unprivileged:1.30.6-alpine`
  with current Alpine packages (fixes for musl, zlib, curl, expat and
  brotli) and no longer runs as root. CI now rejects a web app image that
  runs as root.
- A production build never falls back to dev sign-in: a `/config.json`
  without `authMode` means OIDC.
- zod 3 replaced by zod 4 in the web app (npm audit HIGH,
  GHSA-mrhx-c7xc-m3w5).
