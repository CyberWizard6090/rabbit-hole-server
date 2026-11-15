package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/collab"
	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/middleware"
	"rabbit-hole-server/internal/ratelimit"
	"rabbit-hole-server/internal/service"
)

// DocumentCollabHandler serves the WebSocket room of a document: updates of other editors and presence.
// It reuses the single-use tickets and the session checks of the realtime feeds.
type DocumentCollabHandler struct {
	rt        *RealtimeHandler
	manager   *collab.Manager
	documents *service.DocumentService
	checker   service.PermissionChecker
	opts      collab.Options
	// updates is the budget of one user across all their connections and the REST endpoint. It lives in memory:
	// a database-backed limiter would add a write to every keystroke, and rooms live in one process anyway.
	updates *ratelimit.Window
}

func NewDocumentCollabHandler(
	rt *RealtimeHandler, manager *collab.Manager, documents *service.DocumentService, checker service.PermissionChecker,
) *DocumentCollabHandler {
	return &DocumentCollabHandler{
		rt: rt, manager: manager, documents: documents, checker: checker,
		updates: ratelimit.NewWindow(collab.UpdatesPerMinute, time.Minute),
	}
}

func (h *DocumentCollabHandler) WithOptions(opts collab.Options) *DocumentCollabHandler {
	h.opts = opts
	return h
}

// WithUpdateLimit replaces the per-user budget of updates (tests use a small one).
func (h *DocumentCollabHandler) WithUpdateLimit(max int, period time.Duration) *DocumentCollabHandler {
	h.updates = ratelimit.NewWindow(max, period)
	return h
}

func (h *DocumentCollabHandler) allowUpdate(userID uint) (bool, time.Duration) {
	return h.updates.Allow(strconv.FormatUint(uint64(userID), 10))
}

// RefusedUpdateCost is how many updates of the budget one refused update costs.
const RefusedUpdateCost = 20

// ChargeRefusedUpdate takes RefusedUpdateCost updates from the user's budget (one was already taken).
func (h *DocumentCollabHandler) ChargeRefusedUpdate(userID uint) {
	for i := 1; i < RefusedUpdateCost; i++ {
		h.allowUpdate(userID)
	}
}

// LimitUpdates applies the per-user budget of updates to the REST endpoint; the WebSocket room uses the same one.
func (h *DocumentCollabHandler) LimitUpdates(c *gin.Context) {
	allowed, wait := h.allowUpdate(c.GetUint("userID"))
	if allowed {
		c.Next()
		return
	}
	retryAfter := collab.RetryAfterSeconds(wait)
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	response.Fail(c, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", map[string]any{"retry_after": retryAfter})
	c.Abort()
}

// IssueTicket gives a single-use ticket for this document's room (the same flow as the task feeds).
func (h *DocumentCollabHandler) IssueTicket(c *gin.Context) {
	if id, ok := documentIDParam(c); ok {
		h.rt.issueTicket(c, collab.Topic(id))
	}
}

// Presence lists the people who have the document open right now, one entry per person.
func (h *DocumentCollabHandler) Presence(c *gin.Context) {
	id, ok := documentIDParam(c)
	if !ok {
		return
	}
	response.Success(c, http.StatusOK, h.manager.Presence(id))
}

type documentBackend struct {
	h        *DocumentCollabHandler
	docID    uint
	userID   uint
	version  int
	sessions func(ctx context.Context, userID uint, version int) (bool, error)
}

func (b *documentBackend) AppendUpdate(ctx context.Context, update []byte, touched []string, origin uint64) (int64, error) {
	return b.h.documents.AppendUpdate(collab.WithOrigin(ctx, origin), b.docID, b.userID, update, touched)
}

func (b *documentBackend) AllowUpdate() (bool, time.Duration) { return b.h.allowUpdate(b.userID) }

func (b *documentBackend) Sync(ctx context.Context, since int64) (collab.SyncResult, error) {
	return b.h.documents.Since(ctx, b.docID, since)
}

func (b *documentBackend) Recheck(ctx context.Context) (bool, bool, error) {
	current, err := b.sessions(ctx, b.userID, b.version)
	if err != nil || !current {
		return false, false, err
	}
	read, err := b.h.checker.HasDocumentPermission(ctx, b.userID, b.docID, domain.PermDocRead)
	if err != nil || !read {
		return false, false, err
	}
	write, err := b.h.checker.HasDocumentPermission(ctx, b.userID, b.docID, domain.PermDocUpdate)
	return true, write, err
}

// Connect upgrades to the document room. `since` (optional) is the number of the last update the client has: the
// server sends everything after it right after `ready`.
func (h *DocumentCollabHandler) Connect(c *gin.Context) {
	docID, ok := documentIDParam(c)
	if !ok {
		return
	}
	grant, err := h.rt.tickets.RedeemGrant(c.GetString(middleware.WSTicketKey), collab.Topic(docID))
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("INVALID_TICKET", "invalid or expired ticket", err))
		return
	}
	var since *int64
	if raw := c.Query("since"); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 0 {
			response.HandleError(c, httperrors.BadRequest("INVALID_SINCE", "since must be a non-negative integer", parseErr))
			return
		}
		since = &value
	}

	backend := &documentBackend{h: h, docID: docID, userID: grant.UserID, version: grant.Version, sessions: h.rt.sessionCurrent}
	allowed, canWrite, err := backend.Recheck(c.Request.Context())
	if err != nil {
		response.HandleError(c, httperrors.Internal("PERMISSION_CHECK_FAILED", "internal server error", err))
		return
	}
	if !allowed {
		response.Forbidden(c)
		return
	}
	user, err := h.documents.PublicUser(c.Request.Context(), grant.UserID)
	if err != nil {
		response.HandleError(c, httperrors.Internal("PERMISSION_CHECK_FAILED", "internal server error", err))
		return
	}

	peer, err := h.manager.Join(docID, *user, canWrite)
	if err != nil {
		switch {
		case errors.Is(err, collab.ErrRoomFull), errors.Is(err, collab.ErrTooManyConnections):
			response.HandleError(c, httperrors.TooManyRequests("TOO_MANY_CONNECTIONS", err.Error(), err))
		case errors.Is(err, collab.ErrManagerClosed):
			response.HandleError(c, httperrors.ServiceUnavailable("SHUTTING_DOWN", err.Error(), err))
		default:
			response.HandleError(c, httperrors.Internal("SUBSCRIBE_FAILED", "internal server error", err))
		}
		return
	}
	defer h.manager.Leave(peer)

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{OriginPatterns: h.rt.originPatterns})
	if err != nil {
		slog.Debug("document websocket handshake rejected", "error", err)
		return
	}

	slog.Debug("document websocket connected", "user_id", grant.UserID, "document_id", docID, "conn", peer.ID)
	collab.Serve(c.Request.Context(), conn, h.manager, peer, since, backend, h.opts)
	slog.Debug("document websocket disconnected", "user_id", grant.UserID, "document_id", docID, "conn", peer.ID)
}
