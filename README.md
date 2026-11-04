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
- **Email**: registration sends a confirmation mail, and `POST /auth/forgot-password` +
  `POST /auth/reset-password` reset a password. Links point to the frontend
  (`FRONTEND_URL/verify-email?token=...`, `FRONTEND_URL/reset-password?token=...`); the frontend posts
  the token to `/auth/verify-email` or `/auth/reset-password`. Tokens are random, stored only as a
  SHA-256 hash, work once and expire (48 h and 1 h). `forgot-password` and `resend-verification`
  always answer 202, so they do not reveal which addresses exist, and send at most one mail a minute
  per account. A reset revokes every session. `REQUIRE_VERIFIED_EMAIL=true` makes login answer 403
  `EMAIL_NOT_VERIFIED` until the address is confirmed. Mail language follows `Accept-Language`
  (Russian or English). `MAIL_DRIVER=log` (the default) only logs mail; for a test mailbox run
  `docker compose --profile mail up` and read the mails at http://127.0.0.1:8025 (Mailpit), see
  `.env.example`. Production needs `MAIL_DRIVER=smtp` with `starttls` or `tls`.
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
- **Activity**: `GET /tasks/{id}/activity` returns who changed what on a task, newest first (field
  changes with old and new values, moves between lists, comment and attachment events). Entries are written after
  the change succeeds; a failed write is logged and does not fail the request.
- **Comments**: `POST/GET /tasks/{id}/comments`, `PATCH/DELETE /tasks/{id}/comments/{comment_id}`.
  The body is stored as the client sends it; @mentions are sent as `mention_user_ids` (workspace
  members only). Creating needs `task.update`, only the author edits, and the author or anyone
  with `task.delete` deletes. Comment changes are pushed over the realtime feed.
- **Files**: task attachments (`/tasks/{id}/attachments`), editor images
  (`POST /workspaces/{id}/images`, the upload hook for Inkbrick) and avatars (`POST /profile/avatar`).
  Uploads are `multipart/form-data` with a `file` field. Editor images and avatars are public and
  immutable at `/api/v1/images/{id}`, so their URLs can stay in documents; attachments are private and
  downloaded with the bearer token. Types are detected from the content, SVG is refused, avatars are
  re-encoded to 256x256. Storage is `STORAGE_DRIVER=local` (a directory) or `s3` (MinIO or any
  S3-compatible bucket, see `.env.example`); `docker compose --profile s3 up` starts a local MinIO and
  creates the bucket (inside Compose use `S3_ENDPOINT=minio:9000`).
  Set `PUBLIC_BASE_URL` to the public address of this API so image URLs are absolute, and let the
  reverse proxy pass bodies of at least `MAX_ATTACHMENT_BYTES` (nginx `client_max_body_size`).
- **Checklists**: `/tasks/{id}/checklists` holds named checklists with items (text, resolved by whom and
  when, optional assignee). Up to 20 checklists per task and 200 items per checklist; changing the order
  is one call with the full list of ids (`PUT .../order`). Everything needs `task.update`, reading needs
  `task.read`. Creating and deleting checklists and resolving or reopening items go to the activity log.
- **Task order**: every task has an integer `position` within its list; new tasks go to the end and
  `POST /tasks/{id}/move` reorders them, changes the status or moves them to another list of the
  same workspace. Read the order with `sort=position`.
- **Limits**: request body max 1 MiB (413). Auth rate limits per IP: register 5/min, login 10/min,
  refresh 30/min, logout 30/min, verify-email 20/min, resend-verification and forgot-password 5/min,
  reset-password 10/min (429).
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
