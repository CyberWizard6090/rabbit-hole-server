# Rabbit Hole Server

Go (Gin + GORM + PostgreSQL) backend for the Rabbit Hole task manager.
Frontend: `rabbit-hole-web`.

## Quick start

```bash
cp .env.example .env                 # set POSTGRES_PASSWORD, JWT_SECRET (>= 32 chars)
docker compose up --build            # postgres -> migrate -> api on 127.0.0.1:8080
```

The dev override (`docker-compose.override.yml`) binds Postgres (`5432`) and the API (`8080`)
to `127.0.0.1` only, so nothing is exposed to the local network.

Without Docker:

```bash
make run        # applies migrations, then starts the server
make test       # unit tests; integration tests need TEST_DB_URL (separate database!)
make lint
```

Health check: `GET /healthz`.

## Configuration

See `.env.example`. Important variables: `APP_ENV`, `DB_URL`, `JWT_SECRET`, `JWT_ACCESS_TTL`,
`JWT_REFRESH_TTL`, `ALLOWED_ORIGINS` (CORS), `TRUSTED_PROXIES`.

## API

The contract lives in [`docs/openapi.yaml`](docs/openapi.yaml). Summary of behaviour the frontend
relies on:

- **Envelope**: `{ data, error: {code, message, fields}, pagination: {total,page,limit,pages} }`.
- **Auth**: `POST /auth/login` returns `access_token` and sets an HttpOnly `refresh_token` cookie
  (path `/api/v1/auth`). Call `/auth/refresh` (cookie, `credentials: 'include'`) to rotate.
  `/auth/logout` needs only the cookie, so it works after the access token expired.
- **Pagination**: `page` (default 1, max 1,000,000) and `limit` (default 10; values above 100
  are clamped to 100).
- **Limits**: request body max 1 MiB (413). Auth rate limits per IP: register 5/min, login 10/min,
  refresh 30/min, logout 30/min (429).
- **Authorization**: resource routes check workspace-scoped permissions. IDs that belong to another
  workspace answer 403/404 and never leak data.

## Database migrations

Migrations are embedded (`db/migrations`) and run with `cmd/migrate`
(`up`, `down [-steps N]`, `version`, `force -version N`).

Pre-release policy: the project is not deployed yet, so `000001_init_schema` may be edited in
place. After changing it, recreate the local database:

```bash
docker compose down -v && docker compose up --build
```

**When anyone else works on the repo, tell them about edits to existing migrations** — their
database will otherwise silently drift from the schema.

**From the first release on, migrations are frozen**: existing files are never edited; every
schema change is a new numbered file (`make db-create name=...`). CI runs `up -> down -> up`
to catch broken down scripts.

## Before the first deploy

The production-only work is intentionally deferred; check it off before releasing:

- [ ] TLS reverse proxy in front of the API; `APP_ENV=production` in the prod compose
- [ ] `TRUSTED_PROXIES` set to the proxy's address range
- [ ] Separate DB roles (migrator vs. application) instead of the Postgres superuser
- [ ] Image publishing (GHCR) and pinned image tags
- [ ] Dependency hotfixes (`golang.org/x/net`) via the first develop -> master release
- [ ] Freeze migrations (see above)

## Known follow-ups

- Request `context.Context` is not yet propagated from handlers to repositories (no request-scoped
  cancellation or timeouts on DB calls).
