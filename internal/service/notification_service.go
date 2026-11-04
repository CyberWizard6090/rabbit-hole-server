package service

import (
	"context"
	"sort"

	"rabbit-hole-server/internal/domain"
)

type UnreadCounts struct {
	Total  int64            `json:"total"`
	ByType map[string]int64 `json:"by_type"`
}

type PreferenceView struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Enabled bool   `json:"enabled"`
	Default bool   `json:"default"`
	Usable  bool   `json:"usable"`
}

type WatchView struct {
	Watching bool                 `json:"watching"`
	Watchers []domain.CommentUser `json:"watchers"`
}

type NotificationService struct {
	inbox    domain.NotificationRepository
	settings domain.NotificationSettingsRepository
	watchers domain.WatcherRepository
	channels *ChannelRegistry
}

func NewNotificationService(
	inbox domain.NotificationRepository, settings domain.NotificationSettingsRepository,
	watchers domain.WatcherRepository, channels *ChannelRegistry,
) *NotificationService {
	return &NotificationService{inbox: inbox, settings: settings, watchers: watchers, channels: channels}
}

func (s *NotificationService) List(
	ctx context.Context, userID uint, filter domain.NotificationFilter, limit, offset int,
) ([]domain.Notification, int64, error) {
	if filter.Type != "" {
		if _, ok := domain.NotificationTypeByKey(filter.Type); !ok {
			return nil, 0, domain.ErrUnknownNotificationType
		}
	}
	return s.inbox.List(ctx, userID, filter, limit, offset)
}

func (s *NotificationService) UnreadCounts(ctx context.Context, userID uint) (*UnreadCounts, error) {
	counts, err := s.inbox.UnreadCounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &UnreadCounts{ByType: map[string]int64{}}
	for _, t := range domain.NotificationTypes() {
		out.ByType[t.Key] = counts[t.Key]
		out.Total += counts[t.Key]
	}
	return out, nil
}

func (s *NotificationService) MarkRead(ctx context.Context, userID uint, ids []uint, all bool, typeKey string) (int64, error) {
	if typeKey != "" {
		if _, ok := domain.NotificationTypeByKey(typeKey); !ok {
			return 0, domain.ErrUnknownNotificationType
		}
	}
	return s.inbox.MarkRead(ctx, userID, ids, all, typeKey)
}

func (s *NotificationService) Delete(ctx context.Context, userID, id uint) error {
	return s.inbox.Delete(ctx, userID, id)
}

func (s *NotificationService) Preferences(ctx context.Context, userID uint) ([]PreferenceView, error) {
	stored, err := s.settings.ListPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	explicit := make(map[[2]string]bool, len(stored))
	for _, p := range stored {
		explicit[[2]string{p.Type, p.Channel}] = p.Enabled
	}
	var out []PreferenceView
	for _, ch := range s.channels.All() {
		destinations, err := ch.Destinations(ctx, userID)
		if err != nil {
			return nil, err
		}
		usable := len(destinations) > 0
		for _, t := range domain.NotificationTypes() {
			out = append(out, PreferenceView{
				Type: t.Key, Channel: ch.Kind(), Enabled: effectiveEnabled(t, ch.Kind(), explicit),
				Default: t.DefaultEnabled(ch.Kind()), Usable: usable,
			})
		}
	}
	typeOrder := map[string]int{}
	for i, t := range domain.NotificationTypes() {
		typeOrder[t.Key] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return typeOrder[out[i].Type] < typeOrder[out[j].Type] })
	return out, nil
}

func (s *NotificationService) SavePreferences(ctx context.Context, userID uint, prefs []domain.NotificationPreference) ([]PreferenceView, error) {
	for _, p := range prefs {
		if _, ok := domain.NotificationTypeByKey(p.Type); !ok {
			return nil, domain.ErrUnknownNotificationType
		}
		if _, ok := s.channels.Get(p.Channel); !ok {
			return nil, domain.ErrUnknownChannelKind
		}
	}
	if err := s.settings.SavePreferences(ctx, userID, prefs); err != nil {
		return nil, err
	}
	return s.Preferences(ctx, userID)
}

func (s *NotificationService) Locale(ctx context.Context, userID uint) (string, error) {
	return s.settings.Locale(ctx, userID)
}

func (s *NotificationService) SetLocale(ctx context.Context, userID uint, locale string) error {
	if !domain.IsSupportedNotificationLocale(locale) {
		return domain.ErrUnsupportedNotificationLoc
	}
	return s.settings.SaveLocale(ctx, userID, locale)
}

func (s *NotificationService) Watchers(ctx context.Context, taskID, userID uint) (*WatchView, error) {
	list, err := s.watchers.List(ctx, taskID)
	if err != nil {
		return nil, err
	}
	state, err := s.watchers.State(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	return &WatchView{Watching: state.Watching, Watchers: list}, nil
}

func (s *NotificationService) Watch(ctx context.Context, taskID, userID uint) error {
	return s.watchers.Watch(ctx, taskID, userID)
}

func (s *NotificationService) Unwatch(ctx context.Context, taskID, userID uint) error {
	return s.watchers.Unwatch(ctx, taskID, userID)
}
