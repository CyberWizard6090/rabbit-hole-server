package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type NotificationHandler struct {
	notifications *service.NotificationService
	channels      *service.NotificationChannelService
}

func NewNotificationHandler(n *service.NotificationService, c *service.NotificationChannelService) *NotificationHandler {
	return &NotificationHandler{notifications: n, channels: c}
}

func notificationError(c *gin.Context, code string, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("NOT_FOUND", "not found", err))
	case errors.Is(err, domain.ErrUnknownNotificationType):
		response.HandleError(c, httperrors.BadRequest("UNKNOWN_NOTIFICATION_TYPE", err.Error(), err))
	case errors.Is(err, domain.ErrUnknownChannelKind):
		response.HandleError(c, httperrors.BadRequest("UNKNOWN_CHANNEL", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidDestination):
		response.HandleError(c, httperrors.BadRequest("INVALID_DESTINATION", err.Error(), err))
	case errors.Is(err, domain.ErrChannelNeedsNoDestination):
		response.HandleError(c, httperrors.BadRequest("CHANNEL_HAS_NO_DESTINATION", err.Error(), err))
	case errors.Is(err, domain.ErrUnsupportedNotificationLoc):
		response.HandleError(c, httperrors.BadRequest("UNSUPPORTED_LOCALE", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidToken):
		response.HandleError(c, httperrors.BadRequest("INVALID_TOKEN", err.Error(), err))
	case errors.Is(err, domain.ErrChannelLimitReached):
		response.HandleError(c, httperrors.UnprocessableEntity("CHANNEL_LIMIT_REACHED", err.Error(), err))
	case errors.Is(err, domain.ErrChannelExists):
		response.HandleError(c, httperrors.Conflict("CHANNEL_EXISTS", err.Error(), err))
	case errors.Is(err, domain.ErrChannelAlreadyConfirmed):
		response.HandleError(c, httperrors.Conflict("CHANNEL_ALREADY_CONFIRMED", err.Error(), err))
	case errors.Is(err, domain.ErrConfirmationTooSoon):
		response.HandleError(c, httperrors.TooManyRequests("CONFIRMATION_TOO_SOON", err.Error(), err))
	case errors.Is(err, service.ErrConfirmationMailFailed):
		response.HandleError(c, httperrors.ServiceUnavailable("CONFIRMATION_MAIL_FAILED", err.Error(), err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func idParamOf(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_ID", "invalid "+name, err))
		return 0, false
	}
	return uint(id), true
}

func (h *NotificationHandler) List(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}
	unread, err := parseOptionalBool(c.Query("unread"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FILTER", "unread must be true or false", err))
		return
	}
	items, total, err := h.notifications.List(c.Request.Context(), uid,
		domain.NotificationFilter{UnreadOnly: unread, Type: c.Query("type")}, params.Limit, params.Offset)
	if err != nil {
		notificationError(c, "NOTIFICATIONS_FETCH_FAILED", err)
		return
	}
	pages := (int(total) + params.Limit - 1) / params.Limit
	response.SuccessWithPagination(c, http.StatusOK, items, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: pages,
	})
}

func parseOptionalBool(raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	counts, err := h.notifications.UnreadCounts(c.Request.Context(), uid)
	if err != nil {
		notificationError(c, "NOTIFICATIONS_COUNT_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, counts)
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.MarkReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if !req.All && len(req.IDs) == 0 {
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "pass ids or all=true", nil))
		return
	}
	updated, err := h.notifications.MarkRead(c.Request.Context(), uid, req.IDs, req.All, req.Type)
	if err != nil {
		notificationError(c, "NOTIFICATIONS_READ_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"updated": updated})
}

func (h *NotificationHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	id, ok := idParamOf(c, "notification_id")
	if !ok {
		return
	}
	if err := h.notifications.Delete(c.Request.Context(), uid, id); err != nil {
		notificationError(c, "NOTIFICATION_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "notification deleted"})
}

func (h *NotificationHandler) Preferences(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	prefs, err := h.notifications.Preferences(c.Request.Context(), uid)
	if err != nil {
		notificationError(c, "PREFERENCES_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, prefs)
}

func (h *NotificationHandler) SavePreferences(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.SavePreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	prefs := make([]domain.NotificationPreference, len(req.Preferences))
	for i, p := range req.Preferences {
		prefs[i] = domain.NotificationPreference{Type: p.Type, Channel: p.Channel, Enabled: *p.Enabled}
	}
	saved, err := h.notifications.SavePreferences(c.Request.Context(), uid, prefs)
	if err != nil {
		notificationError(c, "PREFERENCES_SAVE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, saved)
}

func (h *NotificationHandler) GetSettings(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	locale, err := h.notifications.Locale(c.Request.Context(), uid)
	if err != nil {
		notificationError(c, "SETTINGS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"locale": locale})
}

func (h *NotificationHandler) SaveSettings(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.NotificationSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.notifications.SetLocale(c.Request.Context(), uid, req.Locale); err != nil {
		notificationError(c, "SETTINGS_SAVE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"locale": req.Locale})
}

func (h *NotificationHandler) ListChannels(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	channels, err := h.channels.List(c.Request.Context(), uid)
	if err != nil {
		notificationError(c, "CHANNELS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, channels)
}

func (h *NotificationHandler) AddChannel(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.AddChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	locale := service.LocaleFromAcceptLanguage(c.GetHeader("Accept-Language"))
	view, err := h.channels.Add(c.Request.Context(), uid, req.Kind, req.Destination, locale)
	if err != nil {
		notificationError(c, "CHANNEL_CREATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, view)
}

func (h *NotificationHandler) ResendChannel(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	id, ok := idParamOf(c, "channel_id")
	if !ok {
		return
	}
	locale := service.LocaleFromAcceptLanguage(c.GetHeader("Accept-Language"))
	if err := h.channels.Resend(c.Request.Context(), uid, id, locale); err != nil {
		notificationError(c, "CHANNEL_RESEND_FAILED", err)
		return
	}
	response.Success(c, http.StatusAccepted, gin.H{"message": "confirmation sent"})
}

func (h *NotificationHandler) DeleteChannel(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	id, ok := idParamOf(c, "channel_id")
	if !ok {
		return
	}
	if err := h.channels.Delete(c.Request.Context(), uid, id); err != nil {
		notificationError(c, "CHANNEL_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "channel deleted"})
}

func (h *NotificationHandler) ConfirmChannel(c *gin.Context) {
	var req dto.ConfirmChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.channels.Confirm(c.Request.Context(), req.Token); err != nil {
		notificationError(c, "CHANNEL_CONFIRM_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "address confirmed"})
}

func (h *NotificationHandler) Watchers(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	view, err := h.notifications.Watchers(c.Request.Context(), taskID, uid)
	if err != nil {
		notificationError(c, "WATCHERS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, view)
}

func (h *NotificationHandler) Watch(c *gin.Context) {
	h.setWatch(c, true)
}

func (h *NotificationHandler) Unwatch(c *gin.Context) {
	h.setWatch(c, false)
}

func (h *NotificationHandler) setWatch(c *gin.Context, watch bool) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	if watch {
		err = h.notifications.Watch(c.Request.Context(), taskID, uid)
	} else {
		err = h.notifications.Unwatch(c.Request.Context(), taskID, uid)
	}
	if err != nil {
		notificationError(c, "WATCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"watching": watch})
}
