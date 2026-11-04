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
- **Revocation**: access tokens embed the user's token version. Changing the password
  (`POST /profile/password`), `POST /auth/logout-all` and `DELETE /profile` invalidate every token
  issued before, so the frontend must treat a 401 as "sign in again".
- **Members**: workspace members are managed under `/workspaces/:id/members`; only contacts can be
  added, and the owner cannot be re-roled, removed or leave.
- **Pagination**: task lists use keyset pagination (`cursor`, `limit`; the response is
  `{items, next_cursor, prev_cursor, has_next, has_prev}`, plus `total_count` with `with_total=true`).
  `GET /lists/{id}/tasks` also takes `sort` (e.g. `-priority,due_date`, NULLs last) and filters
  (`q`, `status_id`, `priority`, `tag_id`, `no_tags`, `assignee_id`, `unassigned`, `created_*`,
  `due_*`, `start_*`, `has_due`; dates are UTC `YYYY-MM-DD`); invalid values answer 400
  `INVALID_SORT` / `INVALID_FILTER`, and a cursor reused with another sort/filter set answers 400
  `INVALID_PAGINATION`. See `docs/openapi.yaml`. Contact and space lists use `page`
  (1-10000) and `limit` (1-100, default 10). Out-of-range values are rejected with 400
  `INVALID_PAGINATION`.
- **Limits**: request body max 1 MiB (413). Auth rate limits per IP: register 5/min, login 10/min,
  refresh 30/min, logout 30/min (429).
- **Real-time**: task changes can be followed over WebSocket. Request a single-use ticket
  (`POST /lists/:id/events/ticket` or `POST /spaces/:id/events/ticket`) and connect with it; see
  [`docs/realtime.md`](docs/realtime.md).
- **Health**: `GET /healthz` is liveness, `GET /readyz` also pings the database.
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

- Tag, folder and status lists are intentionally unpaginated (bounded per space; used by pickers).
