# Real-time task updates (WebSocket)

The server pushes a message whenever a task in a list or space you follow is
created, updated or deleted, so clients do not have to poll.

Delivery is **best effort**. If a connection drops, or the server tells you that
you were too slow, reload the list through the REST API and reconnect. Events are
notifications, not a log you can replay.

## Connecting

Browsers cannot send an `Authorization` header with a WebSocket, and the access
token must not go into a URL. So connecting takes two steps:

1. Ask for a ticket with your normal bearer token. It needs the `task.read`
   permission on the list or space.

   ```
   POST /api/v1/lists/{list_id}/events/ticket
   POST /api/v1/spaces/{space_id}/events/ticket
   Authorization: Bearer <access token>
   ```

   ```json
   { "data": { "ticket": "…", "expires_in": 30, "topic": "list:5" } }
   ```

2. Open the socket with it, right away. The ticket works **once**, for **that
   topic only**, for about **30 seconds**.

   ```
   GET /api/v1/lists/{list_id}/events?ticket=<ticket>
   GET /api/v1/spaces/{space_id}/events?ticket=<ticket>
   ```

   A space feed carries the events of every list in the space.

```js
async function follow(listId, accessToken, onEvent) {
  const res = await fetch(`/api/v1/lists/${listId}/events/ticket`, {
    method: "POST",
    headers: { Authorization: `Bearer ${accessToken}` },
  });
  const { data } = await res.json();

  const ws = new WebSocket(`wss://${location.host}/api/v1/lists/${listId}/events?ticket=${data.ticket}`);
  ws.onmessage = (m) => onEvent(JSON.parse(m.data));
  ws.onclose = (e) => {
    // 1001 server restarting, 1008 access revoked or too slow, others: network
    // Refetch the list over REST, then ask for a *new* ticket and reconnect.
  };
  return ws;
}
```

Reconnect with a fresh ticket each time; a used ticket is rejected.

## Messages

Every frame is a JSON text message. The first one is `ready`.

| `type` | When | Extra fields |
|---|---|---|
| `ready` | right after the handshake | `topic` |
| `task.created` | a task was created | `task` |
| `task.updated` | a task was changed (fields, status, tags) | `task` |
| `task.deleted` | a task was deleted | none beyond the ids |
| `comment.created` | a comment was added to a task | `comment_id`, `comment` |
| `comment.updated` | a comment was edited | `comment_id`, `comment` |
| `comment.deleted` | a comment was deleted | `comment_id` |

```json
{
  "type": "task.updated",
  "space_id": 2,
  "list_id": 7,
  "task_id": 41,
  "task": { "...": "same shape as GET /tasks/{task_id}" },
  "actor_id": 3,
  "at": "2026-11-03T10:15:00.123Z"
}
```

`actor_id` is the user who made the change, so a client can ignore its own echo.
Clients must not send data messages; doing so closes the connection.

## Notification feed

Every user also has a private feed for the in-app inbox, so that several open tabs
show the same badge and list without polling. It uses the same two-step handshake
and the same origin check, limits and close codes. There is no resource to check:
the ticket belongs to the user who asked for it and the socket only ever carries
that user's events.

```
POST /api/v1/notifications/events/ticket
GET  /api/v1/notifications/events?ticket=<ticket>
```

| `type` | When | Fields |
|---|---|---|
| `ready` | right after the handshake | `topic` (`user:<id>`) |
| `notification.created` | an entry was stored for the user | `notification` (`id`, `type`, `task_id`, `title`, `snippet`, `created_at`), `unread_count` |
| `notification.read` | entries were marked as read (not sent when nothing changed) | `ids` or `all`, `filter_type`, `updated`, `unread_count` |
| `notification.deleted` | an entry was deleted | `notification_id`, `unread_count` |
| `notification.unread_count` | after each event above | `unread_count`, `by_type` |

`unread_count` uses the same rule as `GET /notifications/unread-count`, so entries
about tasks the user can no longer read are not counted, and such entries are never
pushed. `snippet` is plain text, not HTML. The payload is compact on purpose: fetch
`GET /notifications` for the actor and the full details.

## Failed handshakes

| Status | Meaning |
|---|---|
| 401 | missing, unknown, expired, already used, or wrong-topic ticket |
| 403 | the user no longer has `task.read` there, or the `Origin` is not allowed |
| 429 | more than 10 open connections for this user |
| 503 | the server is shutting down |

The `Origin` header must match one of `ALLOWED_ORIGINS` (the same list CORS uses).
Clients that send no `Origin`, such as scripts, are accepted.

## Closing

| Code | Reason |
|---|---|
| 1001 | `server shutting down` |
| 1008 | `access revoked` (removed from the workspace while connected; checked every minute) |
| 1008 | `client too slow` (the client stopped reading and its buffer filled up) |

The server pings every 25 seconds; browsers answer automatically.

## Operational notes

* The hub and the ticket store live in the memory of one server process. With
  several instances behind a load balancer, events made on one instance do not
  reach sockets on another, and a ticket must be redeemed by the instance that
  issued it. Run a single instance, or add a shared broker (for example Postgres
  `LISTEN/NOTIFY` or Redis) before scaling out.
* The `ticket` query parameter is removed from the request before access logging,
  so tickets do not end up in logs.
* Reverse proxies must pass the upgrade headers and use a read timeout longer
  than 25 seconds (nginx: `proxy_read_timeout 70s`, `Upgrade`/`Connection`).
