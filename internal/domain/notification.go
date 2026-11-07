package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	ChannelKindInApp   = "in_app"
	ChannelKindEmail   = "email"
	ChannelKindWebhook = "webhook"
)

const (
	NotificationMention       = "mention"
	NotificationAssigned      = "assigned"
	NotificationComment       = "comment"
	NotificationStatusChanged = "status_changed"
	NotificationDueDateChange = "due_date_changed"
)

const (
	DefaultNotificationLocale = "en"
	MaxChannelsPerUserKind    = 5
	MaxNotificationsPerUser   = 1000
)

var (
	ErrUnknownNotificationType    = errors.New("unknown notification type")
	ErrUnknownChannelKind         = errors.New("unknown notification channel")
	ErrInvalidDestination         = errors.New("destination is not valid for this channel")
	ErrChannelNeedsNoDestination  = errors.New("this channel does not take a destination")
	ErrChannelLimitReached        = errors.New("too many extra destinations for this channel")
	ErrChannelExists              = errors.New("this destination is already added")
	ErrChannelAlreadyConfirmed    = errors.New("this destination is already confirmed")
	ErrChannelHasNoSecret         = errors.New("this channel has no signing secret")
	ErrConfirmationTooSoon        = errors.New("a confirmation was sent a moment ago, try again later")
	ErrUnsupportedNotificationLoc = errors.New("unsupported locale")
)

type NotificationWording struct {
	Subject string
}

type NotificationType struct {
	Key      string
	Priority int
	Defaults map[string]bool
	Wording  map[string]NotificationWording
}

func (t NotificationType) DefaultEnabled(channelKind string) bool {
	return t.Defaults[channelKind]
}

func (t NotificationType) WordingFor(locale string) NotificationWording {
	if w, ok := t.Wording[locale]; ok {
		return w
	}
	return t.Wording[DefaultNotificationLocale]
}

var notificationTypes = []NotificationType{
	{
		Key: NotificationMention, Priority: 0,
		Defaults: map[string]bool{ChannelKindInApp: true, ChannelKindEmail: true, ChannelKindWebhook: true},
		Wording: map[string]NotificationWording{
			"en": {Subject: "%[1]s mentioned you in \"%[2]s\""},
			"ru": {Subject: "Вас упомянули в задаче «%[2]s» (%[1]s)"},
		},
	},
	{
		Key: NotificationAssigned, Priority: 1,
		Defaults: map[string]bool{ChannelKindInApp: true, ChannelKindEmail: true, ChannelKindWebhook: true},
		Wording: map[string]NotificationWording{
			"en": {Subject: "%[1]s assigned you to \"%[2]s\""},
			"ru": {Subject: "Вас назначили на «%[2]s» (%[1]s)"},
		},
	},
	{
		Key: NotificationComment, Priority: 2,
		Defaults: map[string]bool{ChannelKindInApp: true},
		Wording: map[string]NotificationWording{
			"en": {Subject: "%[1]s commented on \"%[2]s\""},
			"ru": {Subject: "Новый комментарий к «%[2]s» (%[1]s)"},
		},
	},
	{
		Key: NotificationStatusChanged, Priority: 3,
		Defaults: map[string]bool{ChannelKindInApp: true},
		Wording: map[string]NotificationWording{
			"en": {Subject: "%[1]s changed the status of \"%[2]s\""},
			"ru": {Subject: "Изменён статус задачи «%[2]s» (%[1]s)"},
		},
	},
	{
		Key: NotificationDueDateChange, Priority: 3,
		Defaults: map[string]bool{ChannelKindInApp: true},
		Wording: map[string]NotificationWording{
			"en": {Subject: "%[1]s changed the due date of \"%[2]s\""},
			"ru": {Subject: "Изменён срок задачи «%[2]s» (%[1]s)"},
		},
	},
}

func NotificationTypes() []NotificationType {
	return append([]NotificationType(nil), notificationTypes...)
}

func NotificationTypeByKey(key string) (NotificationType, bool) {
	for _, t := range notificationTypes {
		if t.Key == key {
			return t, true
		}
	}
	return NotificationType{}, false
}

func IsSupportedNotificationLocale(locale string) bool {
	return locale == "en" || locale == "ru"
}

type Notification struct {
	ID          uint            `json:"id"`
	Type        string          `json:"type"`
	TaskID      uint            `json:"task_id"`
	WorkspaceID uint            `json:"workspace_id"`
	Actor       CommentUser     `json:"actor"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
	ReadAt      *time.Time      `json:"read_at"`
}

type NotificationDraft struct {
	UserID      uint
	Type        string
	TaskID      uint
	ActorID     uint
	WorkspaceID uint
	Payload     json.RawMessage
}

type NotificationRef struct {
	ID        uint
	CreatedAt time.Time
}

type NotificationFilter struct {
	UnreadOnly bool
	Type       string
}

type NotificationRepository interface {
	Create(ctx context.Context, draft NotificationDraft, keep int) (NotificationRef, error)
	List(ctx context.Context, userID uint, filter NotificationFilter, limit, offset int) ([]Notification, int64, error)
	UnreadCounts(ctx context.Context, userID uint) (map[string]int64, error)
	MarkRead(ctx context.Context, userID uint, ids []uint, all bool, typeKey string) (int64, error)
	Delete(ctx context.Context, userID, id uint) error
}

type WatchState struct {
	Watching bool
	Muted    bool
}

type WatcherRepository interface {
	List(ctx context.Context, taskID uint) ([]CommentUser, error)
	State(ctx context.Context, taskID, userID uint) (WatchState, error)
	Watch(ctx context.Context, taskID, userID uint) error
	Unwatch(ctx context.Context, taskID, userID uint) error
	AutoWatch(ctx context.Context, taskID uint, userIDs []uint) error
	ActiveWatcherIDs(ctx context.Context, taskID uint) ([]uint, error)
}

type NotificationPreference struct {
	Type    string
	Channel string
	Enabled bool
}

type NotificationSettingsRepository interface {
	ListPreferences(ctx context.Context, userID uint) ([]NotificationPreference, error)
	SavePreferences(ctx context.Context, userID uint, prefs []NotificationPreference) error
	Locale(ctx context.Context, userID uint) (string, error)
	SaveLocale(ctx context.Context, userID uint, locale string) error
}

type NotificationChannel struct {
	ID          uint       `json:"id"`
	Kind        string     `json:"kind"`
	Destination string     `json:"destination"`
	VerifiedAt  *time.Time `json:"verified_at"`
	CreatedAt   time.Time  `json:"created_at"`
	DisabledAt  *time.Time `json:"disabled_at"`
	LastError   string     `json:"last_error"`
	SecretEnc   string     `json:"-"`
}

type WebhookTarget struct {
	ChannelID uint
	SecretEnc string
}

type WebhookChannelRepository interface {
	SetSecret(ctx context.Context, userID, channelID uint, secretEnc string) error
	ActiveTarget(ctx context.Context, userID uint, destination string) (*WebhookTarget, error)
	RecordDelivery(ctx context.Context, channelID uint, failure string, maxFailures int) error
}

type NotificationChannelRepository interface {
	Create(ctx context.Context, userID uint, kind, destination string, maxPerKind int) (*NotificationChannel, error)
	List(ctx context.Context, userID uint) ([]NotificationChannel, error)
	Get(ctx context.Context, userID, id uint) (*NotificationChannel, error)
	Delete(ctx context.Context, userID, id uint) error
	VerifiedDestinations(ctx context.Context, userID uint, kind string) ([]string, error)
	SaveConfirmation(ctx context.Context, channelID uint, tokenHash string, expiresAt time.Time) error
	DiscardConfirmation(ctx context.Context, tokenHash string) error
	LastConfirmationAt(ctx context.Context, channelID uint) (*time.Time, error)
	Confirm(ctx context.Context, tokenHash string) error
}

type TaskNotificationInfo struct {
	ID          uint
	Title       string
	WorkspaceID uint
}

type AccountMailTarget struct {
	Email    string
	Verified bool
}

type NotificationSourceRepository interface {
	TaskInfo(ctx context.Context, taskID uint) (*TaskNotificationInfo, error)
	StatusName(ctx context.Context, statusID uint) (string, error)
	User(ctx context.Context, userID uint) (*CommentUser, error)
	AccountMail(ctx context.Context, userID uint) (*AccountMailTarget, error)
}
