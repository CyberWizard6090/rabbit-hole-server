# E-mail

Rabbit Hole sends e-mail for account flows (address confirmation, password reset, login address change), for the
confirmation of extra notification addresses and for notification digests of watched tasks. This page covers the
drivers, every setting, local testing with Mailpit, what each message contains, the exact link formats and the API calls
the frontend has to make, and a production checklist.

Contents: [Drivers](#drivers) | [Settings](#settings) | [Testing with Mailpit](#testing-locally-with-mailpit) |
[Flows and links](#flows-links-and-the-api-calls-behind-them) | [Language](#language) | [Limits](#rate-limits-cooldowns-and-token-lifetimes) |
[Production](#production-checklist) | [Troubleshooting](#troubleshooting)

## Drivers

| `MAIL_DRIVER` | Behaviour |
| --- | --- |
| `log` (default) | Nothing is sent. Every message is written to the server log at info level with the masked recipient and the subject; in `development` the full text, including the link, is logged too. Use it only on a developer machine. The server logs a warning at startup when it runs with this driver outside `development`, and `production` and `staging` refuse to start with it. |
| `smtp` | Messages go to the SMTP server from `SMTP_*`. Sending is asynchronous: a bounded queue (256 messages) is drained by a background worker, so an SMTP failure does not fail the HTTP request. It is written to the server log as `mail: cannot send a message`. |

Both drivers send a plain text part and an HTML part.

## Settings

All settings are environment variables (or lines of `.env`). The server validates them at startup and prints every
problem at once, see [Troubleshooting](#troubleshooting).

| Variable | Default | Meaning |
| --- | --- | --- |
| `MAIL_DRIVER` | `log` | `log` or `smtp`. |
| `MAIL_FROM` | `Rabbit Hole <no-reply@localhost>` | Sender. A plain address or `Name <address>`; line breaks are rejected. Use an address of a domain you control (see SPF/DKIM). |
| `SMTP_HOST` | empty | SMTP server host name. Required when `MAIL_DRIVER=smtp`. |
| `SMTP_PORT` | `587` | 1 to 65535. Typical: 587 with `starttls`, 465 with `tls`, 25 with `none`, 1025 for Mailpit. |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | empty | Login for SMTP AUTH. Set both or neither. |
| `SMTP_SECURITY` | `starttls` | `none` (clear text, local test servers only), `starttls` (upgrade a plain connection, TLS 1.2 or newer) or `tls` (TLS from the first byte, usually port 465). |
| `FRONTEND_URL` | `http://localhost:3000` | Base URL of the web client. Every link in a message is built from it, a trailing slash is ignored. Must be `https://` in `staging` and `production`. |
| `EMAIL_VERIFICATION_TTL` | `48h` | How long an address confirmation link works. Go duration such as `30m`, `48h`. |
| `PASSWORD_RESET_TTL` | `1h` | How long a password reset link works. |
| `REQUIRE_VERIFIED_EMAIL` | `false` | When `true`, login answers 403 `EMAIL_NOT_VERIFIED` until the address is confirmed. A password reset also confirms the address, because it proves control of the mailbox. |
| `NOTIFICATION_EMAIL_PER_HOUR` | `30` | Notification e-mails per user per hour. Extra ones are skipped (the in-app entry stays). Account mails (confirmation, reset) are not counted. The counter lives in memory and resets when the server restarts. |

The link of the address change flow expires after a fixed 24 h and the link that confirms an extra notification address
after a fixed 48 h; they have no setting.

## Testing locally with Mailpit

[Mailpit](https://mailpit.axllent.org/) is a fake SMTP server with a web inbox. The Compose override defines it under the
`mail` profile; it listens on `127.0.0.1` only.

```bash
docker compose --profile mail up
```

| What | Address |
| --- | --- |
| Web UI (read the messages) | http://127.0.0.1:8025 |
| SMTP (the API sends here) | `127.0.0.1:1025` |

API running on the host (`make run`), lines for `.env`:

```env
MAIL_DRIVER=smtp
SMTP_HOST=127.0.0.1
SMTP_PORT=1025
SMTP_SECURITY=none
FRONTEND_URL=http://127.0.0.1:3000
```

API running inside Compose (the service name is the host name; set the lines in `.env` next to `docker-compose.yml`):

```env
MAIL_DRIVER=smtp
SMTP_HOST=mailpit
SMTP_PORT=1025
SMTP_SECURITY=none
FRONTEND_URL=http://127.0.0.1:3000
```

Leave `SMTP_USERNAME` and `SMTP_PASSWORD` empty. Then register, request a password reset and so on, open the web UI, and
copy the link from the message (or click it when the frontend runs on `FRONTEND_URL`). Without a frontend you can post the
token yourself, for example:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/auth/verify-email \
  -H 'Content-Type: application/json' -d '{"token":"<token from the link>"}'
```

For a quick look without any SMTP server keep `MAIL_DRIVER=log` and read the link from the server output.

## Flows, links and the API calls behind them

A link is always `FRONTEND_URL` + path + `?token=<token>`. The token is 32 random bytes in URL-safe base64 (no padding), so
it needs no escaping. The frontend page reads `token` from the query string and calls the API below. Only a hash of the
token is stored, a token works once, and a new request for the same purpose replaces the previous token.

All paths below are under `/api/v1`. Responses use the usual envelope (`data` or `error.code` with `error.params`/`error.details`, see `docs/error-codes.md`).

| Flow | Trigger | Link in the message | Frontend call |
| --- | --- | --- | --- |
| Confirm address | `POST /auth/register`, `POST /auth/resend-verification` | `FRONTEND_URL/verify-email?token=...` | `POST /auth/verify-email` |
| Reset password | `POST /auth/forgot-password` | `FRONTEND_URL/reset-password?token=...` | `POST /auth/reset-password` |
| Confirm new login address | `POST /profile/email` | `FRONTEND_URL/confirm-email-change?token=...` | `POST /auth/confirm-email-change` |
| Confirm notification address | `POST /notifications/channels` (kind `email`), `POST /notifications/channels/{id}/resend` | `FRONTEND_URL/confirm-channel?token=...` | `POST /notifications/channels/confirm` |
| Notification digest | a watched task changes | `FRONTEND_URL/tasks/<task id>` and `FRONTEND_URL/settings/notifications` | none (plain links) |

The frontend pages for the first four paths must work without a signed-in user; the matching API calls need no access token.

### Confirm address

- Sent after registration and on request. Registration succeeds even if the mail cannot be sent.
- Request resend: `POST /auth/resend-verification`, body `{"email": "user@example.com"}`. Always `202`, whether or not the
  address exists or is already confirmed, so it cannot be used to find accounts.
- Confirm: `POST /auth/verify-email`, body `{"token": "<token>"}`. `200` with `{"message": "email confirmed"}`.
  Errors: `400 INVALID_TOKEN` (unknown, expired or already used; ask the user to request a new link),
  `400 VALIDATION_FAILED` (missing token), `429` (rate limit).
- With `REQUIRE_VERIFIED_EMAIL=true`, `POST /auth/login` answers `403 EMAIL_NOT_VERIFIED` after the password was checked;
  show a "resend the link" screen.

### Reset password

- Request: `POST /auth/forgot-password`, body `{"email": "user@example.com"}`. Always `202`.
- Reset: `POST /auth/reset-password`, body `{"token": "<token>", "new_password": "<8 to 128 characters>"}`. `200` with
  `{"message": "password changed, sign in with the new password"}`. Errors: `400 INVALID_TOKEN`, `400 WEAK_PASSWORD`,
  `400 VALIDATION_FAILED`, `429`.
- A successful reset signs the user out everywhere (all sessions and access tokens are revoked), so send the user to the
  login page.

### Confirm new login address

- Request (signed in): `POST /profile/email`, body `{"new_email": "new@example.com", "password": "<current password>"}`.
  `202`; the link goes to the new address. Errors: `401 INVALID_CREDENTIALS` (wrong password), `400 SAME_EMAIL`,
  `409 EMAIL_TAKEN`, `429 EMAIL_CHANGE_COOLDOWN` (a request was made less than a minute ago), `429` (rate limit).
  `GET /profile` shows the address as `pending_email`; `DELETE /profile/email-change` cancels the request.
- Confirm: `POST /auth/confirm-email-change`, body `{"token": "<token>"}`. `200` with
  `{"message": "email changed, sign in with the new address"}`. Errors: `400 INVALID_TOKEN`, `409 EMAIL_TAKEN` (someone
  registered the address in the meantime), `429`.
- Confirming signs the user out everywhere and sends a security notice to the old address (with the new address masked as
  `n***@example.com`).

### Confirm notification address

- Request (signed in): `POST /notifications/channels`, body `{"kind": "email", "destination": "other@example.com"}`; the
  link goes to that address. Resend: `POST /notifications/channels/{channel_id}/resend`.
  Errors: `400 UNKNOWN_CHANNEL`, `400 INVALID_DESTINATION`, `409 CHANNEL_EXISTS`, `422 CHANNEL_LIMIT_REACHED` (5 extra
  addresses per kind), `409 CHANNEL_ALREADY_CONFIRMED`, `429 CONFIRMATION_TOO_SOON` (less than a minute since the last
  message), `503 CONFIRMATION_MAIL_FAILED` (the message could not be queued; try again), `429` (rate limit).
- Confirm: `POST /notifications/channels/confirm`, body `{"token": "<token>"}`. `200` with `{"message": "address confirmed"}`.
  Errors: `400 INVALID_TOKEN`, `429`. No access token is needed, so the link works in a browser where the user is not
  signed in.
- Notification e-mails go to the account address (only when it is confirmed) and to every confirmed extra address.

### Notification digests

One message per notification, in the language the user chose in the notification settings (`GET/PUT /notifications/settings`), not in the language of the current request. It links to the task and to the notification
preferences page of the frontend, so the client needs routes `/tasks/<id>` and `/settings/notifications`.

## Language

Account mails (`verify-email`, `reset-password`, `confirm-email-change` and the confirmation of a notification address)
use the `Accept-Language` header of the request that triggers them: the first listed language decides, `ru` and `ru-*` give
Russian, everything else English. Registration, `resend-verification`, `forgot-password`, `POST /profile/email` and
`POST /notifications/channels` all honour it. The security notice that goes to the old address when the login address
changed uses the language of the request that confirmed the change.

## Rate limits, cooldowns and token lifetimes

Rate limits are per client IP and answer `429` with the message `too many requests`. Behind a proxy set
`TRUSTED_PROXIES`, otherwise every client shares the proxy address.

| Endpoint | Limit |
| --- | --- |
| `POST /auth/resend-verification`, `POST /auth/forgot-password` | 5 per minute |
| `POST /auth/verify-email` | 20 per minute |
| `POST /auth/reset-password`, `POST /auth/confirm-email-change` | 10 per minute |
| `POST /profile/email` | 5 per minute |
| `POST /notifications/channels`, `.../{id}/resend` | 20 per minute |
| `POST /notifications/channels/confirm` | 20 per minute |

On top of that, one account receives at most one message of a kind per minute (`resend-verification`, `forgot-password`:
silently skipped; address change and channel resend: `429`, see above).

| Token | Lifetime | Single use |
| --- | --- | --- |
| Confirm address | `EMAIL_VERIFICATION_TTL` (48h) | yes |
| Reset password | `PASSWORD_RESET_TTL` (1h) | yes |
| Confirm new login address | 24h | yes |
| Confirm notification address | 48h | yes |

Every token is also invalidated when a newer one of the same kind is requested. A used, expired or unknown token always
yields the same `400 INVALID_TOKEN`.

## Production checklist

- `APP_ENV=production` (or `staging`). The server then refuses `MAIL_DRIVER=log`, a non-https `FRONTEND_URL`, and
  `SMTP_SECURITY=none` for a remote host.
- `MAIL_DRIVER=smtp` with a real provider; `SMTP_SECURITY=starttls` (port 587) or `tls` (port 465); `SMTP_USERNAME` and
  `SMTP_PASSWORD` from the provider. Keep the password in the secret store, not in the repository.
- `FRONTEND_URL=https://app.example.com`, the public address of the web client. Check that its routes `/verify-email`,
  `/reset-password`, `/confirm-email-change`, `/confirm-channel`, `/tasks/<id>` and `/settings/notifications` exist.
- `MAIL_FROM` on a domain you control, for example `Rabbit Hole <no-reply@example.com>`. Publish SPF, DKIM and DMARC DNS
  records for that domain as your provider describes; without them messages land in spam or are rejected. The provider
  must allow the sender address.
- Decide on `REQUIRE_VERIFIED_EMAIL=true` before users register: it blocks login until the address is confirmed and
  needs working mail delivery and a "resend" screen in the client.
- Set `TRUSTED_PROXIES` to your reverse proxy so the per-IP limits see real client addresses.
- Send a test registration end to end and read the message in a real mailbox.
- Watch the server log for `mail: cannot send a message` (SMTP failures) and for warnings such as `cannot send a verification mail` (for example `mail queue is full`).

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| The server exits at startup printing `invalid configuration, N problems found` | Every wrong or missing variable is listed with a hint. Fix them all and start again. Secret values are never printed. |
| `MAIL_DRIVER=smtp` but nothing arrives and the log shows `mail (log driver, nothing was sent)` | The server runs with `MAIL_DRIVER=log`. Check the variable the server really sees (in Compose, `docker compose config`) and restart. |
| With `MAIL_DRIVER=log` I cannot find the message | It is in the server log (stdout, or `docker compose logs api`), line `mail (log driver, nothing was sent)`; the text and link are printed only when `APP_ENV=development`. |
| Mailpit inbox is empty | Start it with `docker compose --profile mail up`; check `MAIL_DRIVER=smtp`, `SMTP_PORT=1025`, `SMTP_SECURITY=none`; from inside Compose use `SMTP_HOST=mailpit`, from the host `127.0.0.1`. Look at the server log for `mail: cannot send a message`. Also note that `resend-verification` and `forgot-password` send nothing for unknown or already confirmed accounts and within a minute of the previous request. |
| `connection refused` or `i/o timeout` in `mail: cannot send a message` | Wrong host or port, the SMTP server is not running, or a firewall blocks outgoing SMTP (many clouds block port 25; use 587 or 465). In Compose `127.0.0.1` is the API container itself, use the service name. |
| `535` or `authentication failed` | Wrong `SMTP_USERNAME` or `SMTP_PASSWORD`; many providers want an app password or API key as password and a fixed user name such as `apikey`. |
| `tls: first record does not look like a TLS handshake` | `SMTP_SECURITY=tls` on a plain port. Use `starttls` on 587 or `tls` on 465. |
| `x509: certificate signed by unknown authority` or `certificate is valid for ...` | The server certificate does not match `SMTP_HOST` or is not trusted. Use the exact host name from the provider; do not use an IP address. A self-signed server cannot be used with `starttls` or `tls`. |
| `starttls` fails with `server does not support STARTTLS` | The server speaks no TLS on that port; pick the right port or `SMTP_SECURITY=none` only for local servers. |
| Messages arrive in spam or are rejected (`550`) | Sender domain has no SPF/DKIM/DMARC records, or the provider has not verified `MAIL_FROM`. |
| Links open the wrong site or `localhost` | `FRONTEND_URL` is wrong. It is read at startup, so restart after changing it. |
| The link says it is invalid | It was used, expired (see the table above), replaced by a newer one, or the page cut the token from the URL. |
| Notification mails stop | The per-user limit `NOTIFICATION_EMAIL_PER_HOUR` was reached (skipped mails are logged at debug level), or the address is not confirmed. |
