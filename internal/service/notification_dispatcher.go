package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/txhook"
)

const notificationAttempts = 6

var notificationBackoff = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

type NotificationDispatcher struct {
	source   domain.NotificationSourceRepository
	watchers domain.WatcherRepository
	settings domain.NotificationSettingsRepository
	comments domain.CommentRepository
	perms    PermissionChecker
	channels *ChannelRegistry
	mappers  map[string]eventMapper
	logger   *slog.Logger

	outbox   domain.OutboxRepository
	worker   *OutboxWorker
	drainers []*OutboxWorker
}

type NotificationDispatcherDeps struct {
	Source   domain.NotificationSourceRepository
	Watchers domain.WatcherRepository
	Settings domain.NotificationSettingsRepository
	Comments domain.CommentRepository
	Perms    PermissionChecker
	Channels *ChannelRegistry
	Outbox   domain.OutboxRepository
	Logger   *slog.Logger
}

func NewNotificationDispatcher(deps NotificationDispatcherDeps) *NotificationDispatcher {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationDispatcher{
		source: deps.Source, watchers: deps.Watchers, settings: deps.Settings, comments: deps.Comments,
		perms: deps.Perms, channels: deps.Channels, mappers: defaultEventMappers(), logger: logger,
		outbox: deps.Outbox,
	}
}

func (d *NotificationDispatcher) DrainAlso(workers ...*OutboxWorker) {
	d.drainers = append(d.drainers, workers...)
}

func (d *NotificationDispatcher) AttachWorker(worker *OutboxWorker) {
	d.worker = worker
	d.drainers = append(d.drainers, worker)
	worker.Register(OutboxRoute{
		Type: domain.OutboxNotifyEntry, Handler: OutboxHandlerFunc(d.handleEvent), Transactional: true,
		MaxAttempts: notificationAttempts, Backoff: notificationBackoff,
	})
}

type outboxEntry struct {
	TaskID     uint             `json:"task_id"`
	ActorType  domain.ActorType `json:"actor_type"`
	ActorID    uint             `json:"actor_id"`
	ActorLabel string           `json:"actor_label"`
	Action     string           `json:"action"`
	Field      string           `json:"field"`
	OldValue   json.RawMessage  `json:"old_value,omitempty"`
	NewValue   json.RawMessage  `json:"new_value,omitempty"`
}

func encodeEntry(e domain.ActivityEntry) (domain.OutboxEvent, error) {
	actor := e.Actor.Normalized()
	raw, err := json.Marshal(outboxEntry{
		TaskID: e.TaskID, ActorType: actor.Type, ActorID: actor.ID, ActorLabel: actor.Label,
		Action: e.Action, Field: e.Field, OldValue: e.OldValue, NewValue: e.NewValue,
	})
	if err != nil {
		return domain.OutboxEvent{}, err
	}
	return domain.OutboxEvent{
		Type: domain.OutboxNotifyEntry, AggregateType: "task", AggregateID: e.TaskID, Payload: raw,
	}, nil
}

func decodeEntry(raw json.RawMessage) (domain.ActivityEntry, error) {
	var in outboxEntry
	if err := json.Unmarshal(raw, &in); err != nil {
		return domain.ActivityEntry{}, err
	}
	return domain.ActivityEntry{
		TaskID: in.TaskID, Actor: domain.Actor{Type: in.ActorType, ID: in.ActorID, Label: in.ActorLabel},
		Action: in.Action, Field: in.Field, OldValue: in.OldValue, NewValue: in.NewValue,
	}, nil
}

func (d *NotificationDispatcher) handleEvent(ctx context.Context, event domain.OutboxEvent, _ bool) error {
	entry, err := decodeEntry(event.Payload)
	if err != nil {
		return PermanentOutboxError(fmt.Errorf("decode notification event: %w", err))
	}
	return d.handleEntry(ctx, entry)
}

func (d *NotificationDispatcher) handleEntry(ctx context.Context, entry domain.ActivityEntry) error {
	mapper, ok := d.mappers[entry.Action]
	if !ok {
		return nil
	}
	plan, err := mapper(ctx, d, entry)
	if err != nil {
		return fmt.Errorf("map %s: %w", entry.Action, err)
	}
	if plan == nil {
		return nil
	}
	return d.process(ctx, plan)
}

func (d *NotificationDispatcher) Enqueue(ctx context.Context, entries []domain.ActivityEntry) error {
	if len(entries) == 0 || d.outbox == nil {
		return nil
	}
	events := make([]domain.OutboxEvent, 0, len(entries))
	for _, e := range entries {
		event, err := encodeEntry(e)
		if err != nil {
			return fmt.Errorf("encode notification event: %w", err)
		}
		events = append(events, event)
	}
	if err := d.outbox.Add(ctx, events); err != nil {
		return err
	}
	if d.worker != nil {
		txhook.AfterCommit(ctx, d.worker.Poke)
	}
	return nil
}

func (d *NotificationDispatcher) Wait() {
	if len(d.drainers) == 0 {
		return
	}
	if err := DrainOutbox(context.Background(), d.drainers...); err != nil {
		d.logger.Error("notifications: cannot drain the outbox", "error", err)
	}
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
		return err
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
			return err
		}
		if !allowed {
			continue
		}
		if err := d.deliver(ctx, userID, Delivery{
			RecipientID: userID,
			Content: NotificationContent{
				Type: typeDef, TaskID: plan.TaskID, TaskTitle: info.Title, WorkspaceID: info.WorkspaceID,
				Actor: *actor, ActorID: plan.Actor.UserID(), Payload: plan.Payload,
			},
		}); err != nil {
			return err
		}
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

func (d *NotificationDispatcher) deliver(ctx context.Context, userID uint, base Delivery) error {
	stored, err := d.settings.ListPreferences(ctx, userID)
	if err != nil {
		return err
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
			return err
		}
		for _, destination := range destinations {
			delivery := base
			delivery.Destination = destination
			if err := ch.Deliver(ctx, delivery); err != nil {
				if errors.Is(err, ErrEmailThrottled) {
					logDeliveryFailure(userID, ch.Kind(), err)
					continue
				}
				return fmt.Errorf("deliver %s notification: %w", ch.Kind(), err)
			}
		}
	}
	return nil
}

type NotifyingRecorder struct {
	next       ActivityRecorder
	dispatcher *NotificationDispatcher
	tx         TransactionRunner
}

func NewNotifyingRecorder(next ActivityRecorder, dispatcher *NotificationDispatcher, tx TransactionRunner) *NotifyingRecorder {
	return &NotifyingRecorder{next: next, dispatcher: dispatcher, tx: tx}
}

func (r *NotifyingRecorder) Record(ctx context.Context, entries ...domain.ActivityEntry) error {
	write := func(ctx context.Context) error {
		if r.next != nil {
			if err := r.next.Record(ctx, entries...); err != nil {
				return err
			}
		}
		if r.dispatcher != nil {
			return r.dispatcher.Enqueue(ctx, entries)
		}
		return nil
	}
	if r.tx == nil {
		return write(ctx)
	}
	return r.tx.Run(ctx, write)
}
