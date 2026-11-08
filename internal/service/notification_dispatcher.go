package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"rabbit-hole-server/internal/domain"
)

const (
	notificationQueueSize = 1024
	notificationWorkLimit = 30 * time.Second
)

type NotificationDispatcher struct {
	source   domain.NotificationSourceRepository
	watchers domain.WatcherRepository
	settings domain.NotificationSettingsRepository
	comments domain.CommentRepository
	perms    PermissionChecker
	channels *ChannelRegistry
	mappers  map[string]eventMapper
	logger   *slog.Logger

	queue  chan []domain.ActivityEntry
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.RWMutex
	closed  bool
	pending sync.WaitGroup
}

type NotificationDispatcherDeps struct {
	Source   domain.NotificationSourceRepository
	Watchers domain.WatcherRepository
	Settings domain.NotificationSettingsRepository
	Comments domain.CommentRepository
	Perms    PermissionChecker
	Channels *ChannelRegistry
	Logger   *slog.Logger
}

func NewNotificationDispatcher(deps NotificationDispatcherDeps) *NotificationDispatcher {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &NotificationDispatcher{
		source: deps.Source, watchers: deps.Watchers, settings: deps.Settings, comments: deps.Comments,
		perms: deps.Perms, channels: deps.Channels, mappers: defaultEventMappers(), logger: logger,
		queue: make(chan []domain.ActivityEntry, notificationQueueSize),
		ctx:   ctx, cancel: cancel, done: make(chan struct{}),
	}
	go d.run()
	return d
}

func (d *NotificationDispatcher) run() {
	defer close(d.done)
	for {
		select {
		case <-d.ctx.Done():
			for range d.queue {
				d.pending.Done()
			}
			return
		case batch, ok := <-d.queue:
			if !ok {
				return
			}
			d.handleBatch(batch)
			d.pending.Done()
		}
	}
}

func (d *NotificationDispatcher) handleBatch(batch []domain.ActivityEntry) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("notifications: recovered from a panic", "panic", fmt.Sprint(r))
		}
	}()
	ctx, cancel := context.WithTimeout(d.ctx, notificationWorkLimit)
	defer cancel()
	for _, entry := range batch {
		d.handleEntry(ctx, entry)
	}
}

func (d *NotificationDispatcher) handleEntry(ctx context.Context, entry domain.ActivityEntry) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("notifications: recovered from a panic", "action", entry.Action, "panic", fmt.Sprint(r))
		}
	}()
	mapper, ok := d.mappers[entry.Action]
	if !ok {
		return
	}
	plan, err := mapper(ctx, d, entry)
	if err != nil {
		d.logger.Warn("notifications: cannot map an activity entry", "action", entry.Action, "task_id", entry.TaskID, "error", err)
		return
	}
	if plan == nil {
		return
	}
	if err := d.process(ctx, plan); err != nil {
		d.logger.Warn("notifications: cannot process an event", "action", entry.Action, "task_id", entry.TaskID, "error", err)
	}
}

func (d *NotificationDispatcher) Enqueue(entries []domain.ActivityEntry) {
	if len(entries) == 0 {
		return
	}
	batch := append([]domain.ActivityEntry(nil), entries...)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return
	}
	d.pending.Add(1)
	select {
	case d.queue <- batch:
	default:
		d.pending.Done()
		d.logger.Warn("notifications: queue is full, dropping an event", "task_id", batch[0].TaskID)
	}
}

func (d *NotificationDispatcher) Wait() {
	d.pending.Wait()
	for _, ch := range d.channels.All() {
		if w, ok := ch.(interface{ Wait() }); ok {
			w.Wait()
		}
	}
}

func (d *NotificationDispatcher) Close() {
	if err := d.Shutdown(context.Background()); err != nil {
		d.logger.Error("notifications: cannot shut down dispatcher", "error", err)
	}
}

func (d *NotificationDispatcher) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.queue)
	}
	d.mu.Unlock()

	select {
	case <-d.done:
		d.cancel()
	case <-ctx.Done():
		d.cancel()
		return ctx.Err()
	}
	var shutdownErr error
	for _, ch := range d.channels.All() {
		if s, ok := ch.(interface{ Shutdown(context.Context) error }); ok {
			shutdownErr = errors.Join(shutdownErr, s.Shutdown(ctx))
		}
	}
	return shutdownErr
}

func stronger(current string, candidate string) bool {
	cur, ok := domain.NotificationTypeByKey(current)
	if !ok {
		return true
	}
	cand, ok := domain.NotificationTypeByKey(candidate)
	if !ok {
		return false
	}
	return cand.Priority < cur.Priority
}

func resolveRecipients(plan *notificationPlan, watchers []uint) map[uint]string {
	chosen := map[uint]string{}
	consider := func(userID uint, typeKey string) {
		if userID == 0 || (plan.Actor.IsUser() && userID == plan.Actor.ID) {
			return
		}
		if current, ok := chosen[userID]; !ok || stronger(current, typeKey) {
			chosen[userID] = typeKey
		}
	}
	for _, t := range plan.Direct {
		consider(t.UserID, t.Type)
	}
	if plan.WatcherType != "" {
		for _, id := range watchers {
			consider(id, plan.WatcherType)
		}
	}
	return chosen
}

func effectiveEnabled(t domain.NotificationType, kind string, prefs map[[2]string]bool) bool {
	if v, ok := prefs[[2]string{t.Key, kind}]; ok {
		return v
	}
	return t.DefaultEnabled(kind)
}

func (d *NotificationDispatcher) process(ctx context.Context, plan *notificationPlan) error {
	if plan.TaskID == 0 {
		return d.processWorkspaceEvent(ctx, plan)
	}
	if err := d.watchers.AutoWatch(ctx, plan.TaskID, plan.AutoWatch); err != nil {
		d.logger.Warn("notifications: cannot auto-watch", "task_id", plan.TaskID, "error", err)
	}
	var watchers []uint
	if plan.WatcherType != "" {
		ids, err := d.watchers.ActiveWatcherIDs(ctx, plan.TaskID)
		if err != nil {
			return err
		}
		watchers = ids
	}
	recipients := resolveRecipients(plan, watchers)
	if len(recipients) == 0 {
		return nil
	}
	info, err := d.source.TaskInfo(ctx, plan.TaskID)
	if err != nil {
		return err
	}
	actor, err := d.actorFor(ctx, plan)
	if err != nil {
		return err
	}
	ids := make([]uint, 0, len(recipients))
	for id := range recipients {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, userID := range ids {
		typeDef, ok := domain.NotificationTypeByKey(recipients[userID])
		if !ok {
			continue
		}
		allowed, err := d.perms.HasTaskPermission(ctx, userID, plan.TaskID, domain.PermTaskRead)
		if err != nil {
			d.logger.Warn("notifications: cannot check access", "user_id", userID, "error", err)
			continue
		}
		if !allowed {
			continue
		}
		d.deliver(ctx, userID, Delivery{
			RecipientID: userID,
			Content: NotificationContent{
				Type: typeDef, TaskID: plan.TaskID, TaskTitle: info.Title, WorkspaceID: info.WorkspaceID,
				Actor: *actor, ActorID: plan.Actor.UserID(), Payload: plan.Payload,
			},
		})
	}
	return nil
}

func (d *NotificationDispatcher) actorFor(ctx context.Context, plan *notificationPlan) (*domain.CommentUser, error) {
	if plan.Actor.IsUser() {
		return d.source.User(ctx, plan.Actor.ID)
	}
	label := plan.Actor.Normalized().Label
	payload := make(map[string]any, len(plan.Payload)+2)
	for k, v := range plan.Payload {
		payload[k] = v
	}
	payload["actor_type"] = string(plan.Actor.Type)
	payload["actor_name"] = label
	plan.Payload = payload
	return &domain.CommentUser{FirstName: label}, nil
}

func (d *NotificationDispatcher) deliver(ctx context.Context, userID uint, base Delivery) {
	stored, err := d.settings.ListPreferences(ctx, userID)
	if err != nil {
		d.logger.Warn("notifications: cannot load preferences", "user_id", userID, "error", err)
		return
	}
	prefs := make(map[[2]string]bool, len(stored))
	for _, p := range stored {
		prefs[[2]string{p.Type, p.Channel}] = p.Enabled
	}
	locale, err := d.settings.Locale(ctx, userID)
	if err != nil {
		locale = domain.DefaultNotificationLocale
	}
	base.Locale = locale
	for _, ch := range d.channels.All() {
		if !effectiveEnabled(base.Content.Type, ch.Kind(), prefs) {
			continue
		}
		destinations, err := ch.Destinations(ctx, userID)
		if err != nil {
			logDeliveryFailure(userID, ch.Kind(), err)
			continue
		}
		for _, destination := range destinations {
			delivery := base
			delivery.Destination = destination
			if err := ch.Deliver(ctx, delivery); err != nil {
				logDeliveryFailure(userID, ch.Kind(), err)
			}
		}
	}
}

type NotifyingRecorder struct {
	next       ActivityRecorder
	dispatcher *NotificationDispatcher
}

func NewNotifyingRecorder(next ActivityRecorder, dispatcher *NotificationDispatcher) *NotifyingRecorder {
	return &NotifyingRecorder{next: next, dispatcher: dispatcher}
}

func (r *NotifyingRecorder) Record(ctx context.Context, entries ...domain.ActivityEntry) {
	if r.next != nil {
		r.next.Record(ctx, entries...)
	}
	if r.dispatcher != nil {
		r.dispatcher.Enqueue(entries)
	}
}
