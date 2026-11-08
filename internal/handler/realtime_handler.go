package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/middleware"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

const permissionTaskRead = "task.read"

type RealtimeHandler struct {
	hub            *realtime.Hub
	tickets        *realtime.TicketStore
	checker        service.PermissionChecker
	originPatterns []string
	opts           realtime.Options
	versions       TokenVersionSource
}

type TokenVersionSource interface {
	CurrentTokenVersion(ctx context.Context, userID uint) (int, error)
}

func (h *RealtimeHandler) WithTokenVersions(source TokenVersionSource) *RealtimeHandler {
	h.versions = source
	return h
}

func (h *RealtimeHandler) sessionCurrent(ctx context.Context, userID uint, version int) (bool, error) {
	if h.versions == nil {
		return true, nil
	}
	current, err := h.versions.CurrentTokenVersion(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current == version, nil
}

func NewRealtimeHandler(
	hub *realtime.Hub,
	tickets *realtime.TicketStore,
	checker service.PermissionChecker,
	allowedOrigins []string,
) *RealtimeHandler {
	return &RealtimeHandler{
		hub:            hub,
		tickets:        tickets,
		checker:        checker,
		originPatterns: originHosts(allowedOrigins),
	}
}

func (h *RealtimeHandler) WithOptions(opts realtime.Options) *RealtimeHandler {
	h.opts = opts
	return h
}

func originHosts(origins []string) []string {
	hosts := make([]string, 0, len(origins))
	for _, origin := range origins {
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			hosts = append(hosts, u.Host)
		}
	}
	return hosts
}

func idParam(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_"+strings.ToUpper(name), "invalid "+name, err))
		return 0, false
	}
	return uint(id), true
}

func (h *RealtimeHandler) issueTicket(c *gin.Context, topic string) {
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	ticket, err := h.tickets.IssueVersioned(userID, topic, contextutil.GetTokenVersion(c))
	if err != nil {
		if errors.Is(err, realtime.ErrTooManyTickets) {
			response.HandleError(c, httperrors.TooManyRequests("TOO_MANY_TICKETS", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("TICKET_ISSUE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{
		"ticket":     ticket,
		"expires_in": int(h.tickets.TTL().Seconds()),
		"topic":      topic,
	})
}

func (h *RealtimeHandler) IssueListTicket(c *gin.Context) {
	if listID, ok := idParam(c, "list_id"); ok {
		h.issueTicket(c, realtime.ListTopic(listID))
	}
}

func (h *RealtimeHandler) IssueSpaceTicket(c *gin.Context) {
	if spaceID, ok := idParam(c, "space_id"); ok {
		h.issueTicket(c, realtime.SpaceTopic(spaceID))
	}
}

func (h *RealtimeHandler) IssueNotificationTicket(c *gin.Context) {
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	h.issueTicket(c, realtime.UserTopic(userID))
}

func (h *RealtimeHandler) ConnectNotifications(c *gin.Context) {
	redeem := func(raw string) (realtime.Grant, string, error) {
		grant, err := h.tickets.RedeemUserGrant(raw)
		return grant, realtime.UserTopic(grant.UserID), err
	}
	h.connect(c, redeem, func(uint) (bool, error) { return true, nil })
}

func (h *RealtimeHandler) ConnectList(c *gin.Context) {
	listID, ok := idParam(c, "list_id")
	if !ok {
		return
	}
	h.connect(c, redeemFor(h, realtime.ListTopic(listID)), func(userID uint) (bool, error) {
		return h.checker.HasListPermission(c.Request.Context(), userID, listID, permissionTaskRead)
	})
}

func (h *RealtimeHandler) ConnectSpace(c *gin.Context) {
	spaceID, ok := idParam(c, "space_id")
	if !ok {
		return
	}
	h.connect(c, redeemFor(h, realtime.SpaceTopic(spaceID)), func(userID uint) (bool, error) {
		return h.checker.HasSpacePermission(c.Request.Context(), userID, spaceID, permissionTaskRead)
	})
}

func redeemFor(h *RealtimeHandler, topic string) func(string) (realtime.Grant, string, error) {
	return func(raw string) (realtime.Grant, string, error) {
		grant, err := h.tickets.RedeemGrant(raw, topic)
		return grant, topic, err
	}
}

func (h *RealtimeHandler) connect(
	c *gin.Context, redeem func(raw string) (realtime.Grant, string, error), resourceAllowed func(userID uint) (bool, error),
) {
	grant, topic, err := redeem(c.GetString(middleware.WSTicketKey))
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("INVALID_TICKET", "invalid or expired ticket", err))
		return
	}
	userID := grant.UserID
	authorize := func(uid uint) (bool, error) {
		current, err := h.sessionCurrent(c.Request.Context(), uid, grant.Version)
		if err != nil || !current {
			return false, err
		}
		return resourceAllowed(uid)
	}

	allowed, err := authorize(userID)
	if err != nil {
		response.HandleError(c, httperrors.Internal("PERMISSION_CHECK_FAILED", "internal server error", err))
		return
	}
	if !allowed {
		response.Forbidden(c)
		return
	}

	sub, err := h.hub.Subscribe(userID, topic)
	if err != nil {
		switch {
		case errors.Is(err, realtime.ErrTooManyConnections):
			response.HandleError(c, httperrors.TooManyRequests("TOO_MANY_CONNECTIONS", err.Error(), err))
		case errors.Is(err, realtime.ErrHubClosed):
			response.HandleError(c, httperrors.ServiceUnavailable("SHUTTING_DOWN", err.Error(), err))
		default:
			response.HandleError(c, httperrors.Internal("SUBSCRIBE_FAILED", "internal server error", err))
		}
		return
	}
	defer h.hub.Unsubscribe(sub)

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{OriginPatterns: h.originPatterns})
	if err != nil {
		slog.Debug("websocket handshake rejected", "error", err)
		return
	}

	slog.Debug("websocket connected", "user_id", userID, "topic", topic)
	realtime.Serve(c.Request.Context(), conn, sub, func(_ context.Context) bool {
		stillAllowed, err := authorize(userID)
		return err == nil && stillAllowed
	}, h.opts)
	slog.Debug("websocket disconnected", "user_id", userID, "topic", topic)
}
