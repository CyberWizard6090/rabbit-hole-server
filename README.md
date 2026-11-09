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
`JWT_REFRESH_TTL`, `ALLOWED_ORIGINS` (CORS), `TRUSTED_PROXIES`. The server validates the whole
configuration at startup and refuses to start with one error that lists every problem (variable, what is
wrong, how to fix it); secret values are never printed. `go run ./cmd/migrate` checks the database settings the same way.

## API

The contract lives in [`docs/openapi.yaml`](docs/openapi.yaml). Summary of behaviour the frontend
relies on:

- **Envelope**: `{ data, error: {code, params, details, request_id}, pagination: {total,page,limit,pages} }`; errors carry no text, the client translates `code` (see `docs/error-codes.md`).
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
  `.env.example`. Production needs `MAIL_DRIVER=smtp` with `starttls` or `tls`. Full setup, links and
  troubleshooting: [`docs/mail.md`](docs/mail.md).
  A signed-in user changes the login address with `POST /profile/email` (password required): a 24 h
  single-use link goes to the new address (`FRONTEND_URL/confirm-email-change?token=...`, posted to
  `/auth/confirm-email-change`), `DELETE /profile/email-change` cancels it, `GET /profile` shows
  `pending_email`. Confirming revokes every session like a password change and warns the old address.
- **Contacts and privacy**: contacts are consent based (`POST /contacts` sends a request that looks the
  same for every id, the other user accepts or declines under `/contacts/requests`); only accepted
  contacts can be added to a workspace. Other users are exposed as id, username, name and avatar only
  (never e-mail, bio or time zone), and `/users/search` does not match e-mail addresses.
- **Members**: workspace members are managed under `/workspaces/:id/members`; only accepted contacts can be
  added, and the owner cannot be re-roled, removed or leave.
- **Pagination**: task lists use keyset pagination (`cursor`, `limit`; the response is
  `{items, next_cursor, prev_cursor, has_next, has_prev}`, plus `total_count` with `with_total=true`).
  `GET /lists/{id}/tasks` also takes `sort` (e.g. `-priority,due_date`, NULLs last) and filters
  (`q`, `status_id`, `priority`, `tag_id`, `no_tags`, `assignee_id`, `unassigned`, `created_*`,
  `due_*`, `start_*`, `has_due`, `has_open_checklist_items`; dates are UTC `YYYY-MM-DD`); invalid values answer 400
  `INVALID_SORT` / `INVALID_FILTER`, and a cursor reused with another sort/filter set answers 400
  `INVALID_PAGINATION`. See `docs/openapi.yaml`. Contact and space lists use `page`
  (1-10000) and `limit` (1-100, default 10). Out-of-range values are rejected with 400
  `INVALID_PAGINATION`.
- **Search**: `GET /workspaces/{id}/search?q=&types=tasks,comments,checklist_items&space_id=&page=&limit=`
  searches live tasks (title, description), comments and checklist items of one workspace (guests
  may search). `q` has 2-100 characters; 3+ characters also match typos (pg_trgm, GIN indexes).
  Results carry a score, task context and a plain-text snippet with rune offsets of the matches;
  `pagination.total` is the match count. E-mail addresses are never searched. See `docs/openapi.yaml`.
- **Forms**: custom fields, task templates and forms with immutable published versions. A public submission (`POST /public/forms/{slug}/submissions`, idempotent, rate limited per IP and per form) can create a task in one transaction; the task is logged in the activity feed and notifications only after the commit. Failed task creation keeps the submission and can be retried.
  Forms also have presentation settings (button text, success message, https redirect, accent color, logo and cover images), `closes_at` and `max_submissions` restrictions, in-app notifications for chosen members (`notify_on_submit`), conditional questions (`settings.visible_if`) and public file uploads for `file` questions (`POST /public/forms/{slug}/uploads`, sniffed types, `FORM_UPLOAD_MAX_BYTES`, `FORM_UPLOAD_TTL`); the files become task attachments or stay downloadable from the submission.
- **Form builder API**: new questions are appended (or inserted with a shift) under a row lock, `POST /workspaces/{id}/forms` creates a form with its questions and task configuration atomically, `GET /form-templates` returns localized presets, submissions are paginated and filterable, and one priority scale (1 low .. 5 critical, names `low`..`critical`) is used by tasks, templates and forms. The answer formats, question settings and task-config precedence (`mappings > template > task_defaults`) are documented in `docs/openapi.yaml`.
- **Task, list and field rules**: required custom fields are enforced on task creation (`custom_fields` in the body, template apply and form tasks) and cannot be cleared; tags can be removed with `remove_tag_ids`; deleting a status can move its tasks with `replacement_status_id` (refused while a published form uses it); every new list gets the default statuses; lists move between folders with `folder_id`; editing a custom field that has values is rejected when it would invalidate them; tasks expose `subtask_count` and the list endpoint filters by `parent_id` / `has_parent`; templates carry an absolute `due_date` or a relative `due_in_days`.
- **Soft delete**: deleting a workspace, space, folder, list or task soft-deletes everything below it in the same transaction with one timestamp (tasks take their subtasks). Reads additionally check the whole ancestor chain through one shared predicate, so tasks, comments, attachments, checklists, watchers, activity, notifications, realtime tickets, public forms (submissions and uploads) and public images of a deleted parent answer 404/403. `PATCH /tasks/{id}` locks the task and writes only the changed columns, so it never reverts a concurrent move, status change or delete; the database enforces that a task's status belongs to its list and the list to its space.
- **Activity**: `GET /tasks/{id}/activity` returns who changed what on a task, newest first (field
  changes with old and new values, moves between lists, comment and attachment events). Entries are written after
  the change succeeds; a failed write is logged and does not fail the request.
- **Event actors**: every activity entry and every task records who or what initiated it (`actor_type` user, form, automation, integration or system, a nullable `actor_id` and a `actor_label` snapshot). Tasks created from a public form are attributed to the form (`created_by`, `task.created` actor), keep the form creator or the configured `owner_id` as technical owner, never suppress notifications for that owner, and may carry a `requester` (signed-in respondent or mapped name and e-mail, visible to members with `task.update` only). A choice answer can be mapped to the task assignee.
- **Comments**: `POST/GET /tasks/{id}/comments`, `PATCH/DELETE /tasks/{id}/comments/{comment_id}`.
  The body is stored as the client sends it; @mentions are sent as `mention_user_ids` (workspace
  members only). Creating needs `task.update`, only the author edits, and the author or anyone
  with `task.delete` deletes. Comment changes are pushed over the realtime feed.
- **Files**: task attachments (`/tasks/{id}/attachments`), editor images
  (`POST /workspaces/{id}/images`, the upload hook for Inkbrick), avatars (`POST /profile/avatar`) and workspace avatars (`POST/DELETE /workspaces/{id}/avatar`, `workspace.update`, shown as `avatar_url` on every workspace).
  Uploads are `multipart/form-data` with a `file` field. Editor images and avatars are public and
  immutable at `/api/v1/images/{id}`, so their URLs can stay in documents; attachments are private and
  downloaded with the bearer token. Types are detected from the content, SVG is refused, avatars are
  re-encoded to 256x256. Storage is `STORAGE_DRIVER=local` (a directory) or `s3` (MinIO or any
  S3-compatible bucket, see `.env.example`); `docker compose --profile s3 up` starts a local MinIO and
  creates the bucket (inside Compose use `S3_ENDPOINT=minio:9000`).
  `POST /workspaces/{id}/images/rehost` (`{url}`) is the `images.rehost` hook: the server downloads a
  remote image through the SSRF-safe client in `internal/safehttp` (public addresses only, DNS pinned
  per connection, at most 3 re-validated redirects, 10 s, `REHOST_MAX_BYTES`) and stores it like an
  upload; anything unsafe or not an image answers 422 `REHOST_REFUSED`.
  Set `PUBLIC_BASE_URL` to the public address of this API so image URLs are absolute, and let the
  reverse proxy pass bodies of at least `MAX_ATTACHMENT_BYTES` (nginx `client_max_body_size`).
- **Checklists**: `/tasks/{id}/checklists` holds named checklists with items (text, resolved by whom and
  when, optional assignee). Up to 20 checklists per task and 200 items per checklist; changing the order
  is one call with the full list of ids (`PUT .../order`). Everything needs `task.update`, reading needs
  `task.read`. Creating and deleting checklists and resolving or reopening items go to the activity log.
  Every task carries `checklist_items_total` / `checklist_items_resolved` (a "2/5" badge, computed in SQL,
  subtasks not counted); the task list filter `has_open_checklist_items` keeps tasks with / without open items.
- **Notifications**: people watch tasks (`PUT/DELETE /tasks/{id}/watch`, `GET /tasks/{id}/watchers`; creators,
  assignees, commenters and mentioned users watch automatically, an explicit unwatch stays). Mentions,
  assignments, comments, status and due date changes become notifications of five types, delivered to the
  in-app inbox (`/notifications`) and by e-mail according to per-type preferences
  (`/notifications/preferences`). Extra e-mail addresses (`/notifications/channels`) receive nothing until
  the owner confirms them with an emailed link. `NOTIFICATION_EMAIL_PER_HOUR` caps mails per user. A `webhook`
  channel (https URL, SSRF-safe, confirmed with a signed single-use token POSTed to it) delivers the same events as
  JSON signed with `X-Rabbit-Hole-Signature` (HMAC-SHA256, per-channel secret shown once, rotatable); it is disabled
  after `NOTIFICATION_WEBHOOK_MAX_FAILURES` consecutive failures. Open tabs stay in sync over the per-user
  WebSocket feed `GET /notifications/events` (single-use ticket from `POST /notifications/events/ticket`):
  `notification.created`, `.read`, `.deleted` and `.unread_count`.
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
(`up`, `down -steps N`, `down -all`, `version`, `force -version N`). `down` without `-steps` is refused;
`down -all` rolls back everything and is refused when `APP_ENV=production`. The server checks the schema
version at startup and exits with a clear message when the database is behind or dirty (run `migrate -action up`).

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

## Deployment

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

- `docker-compose.prod.yml` sets `APP_ENV=production` (release mode, JSON logs, strict config validation, secure
  cookies). Without it `docker-compose.yml` defaults to `development`; never expose that mode. `staging` also runs
  in release mode.
- `TRUSTED_PROXIES` is empty by default: `X-Forwarded-For` is ignored and the TCP peer is the client. Behind a
  reverse proxy set it to the proxy address(es), never to `0.0.0.0/0`; the startup log lists what is trusted.
- Rate limits, the sign-in lock and the argon2 gate are per process (a restart resets them, N replicas allow N times
  the budget). IPv6 clients are limited per /64.
- Sign-in: at most `AUTH_HASH_CONCURRENCY` password hashes run at once (default: CPUs), extra requests get 503 with
  `Retry-After`; repeated failures for one e-mail address delay further attempts (`AUTH_LOGIN_*`).
- On SIGTERM the server waits up to `SHUTDOWN_TIMEOUT` (30s); compose gives the container `stop_grace_period: 45s`.
- The Go version of the image comes from `go.mod` (`make docker-build`, CI passes it as the `GO_VERSION` build arg).
- `LOG_LEVEL` (debug, info, warn, error) overrides the default level; logs never contain SQL parameter values, tokens,
  passwords or full e-mail addresses.
- PostgreSQL sessions get `statement_timeout`, `lock_timeout` and `idle_in_transaction_session_timeout` from `DB_*`.
- Account revocation (logout everywhere, password change or reset, account deletion, removal from a workspace)
  closes the user's WebSocket feeds immediately; open feeds are also revalidated every two minutes.

## Before the first deploy

The production-only work is intentionally deferred; check it off before releasing:

- [ ] TLS reverse proxy in front of the API (the prod compose already sets `APP_ENV=production`)
- [ ] `TRUSTED_PROXIES` set to the proxy's address range
- [ ] Separate DB roles (migrator vs. application) instead of the Postgres superuser
- [ ] Image publishing (GHCR) and pinned image tags
- [ ] Dependency hotfixes (`golang.org/x/net`) via the first develop -> master release
- [ ] Freeze migrations (see above)

## Known follow-ups

- Tag, folder and status lists are intentionally unpaginated (bounded per space; used by pickers).
