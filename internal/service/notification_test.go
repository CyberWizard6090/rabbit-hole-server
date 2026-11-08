package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/mail"
)

type fakeSource struct {
	accounts map[uint]domain.AccountMailTarget
}

func (f *fakeSource) TaskInfo(_ context.Context, id uint) (*domain.TaskNotificationInfo, error) {
	return &domain.TaskNotificationInfo{ID: id, Title: "Fix <b>login</b>", WorkspaceID: 9}, nil
}

func (f *fakeSource) StatusName(context.Context, uint) (string, error) { return "Done", nil }

func (f *fakeSource) User(_ context.Context, id uint) (*domain.CommentUser, error) {
	return &domain.CommentUser{ID: id, Username: "actor", FirstName: "Ann"}, nil
}

func (f *fakeSource) AccountMail(_ context.Context, id uint) (*domain.AccountMailTarget, error) {
	acc, ok := f.accounts[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &acc, nil
}

type fakeWatchers struct {
	mu     sync.Mutex
	active []uint
	auto   []uint
}

func (f *fakeWatchers) List(context.Context, uint) ([]domain.CommentUser, error) { return nil, nil }
func (f *fakeWatchers) State(context.Context, uint, uint) (domain.WatchState, error) {
	return domain.WatchState{}, nil
}
func (f *fakeWatchers) Watch(context.Context, uint, uint) error   { return nil }
func (f *fakeWatchers) Unwatch(context.Context, uint, uint) error { return nil }
func (f *fakeWatchers) AutoWatch(_ context.Context, _ uint, ids []uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auto = append(f.auto, ids...)
	return nil
}
func (f *fakeWatchers) ActiveWatcherIDs(context.Context, uint) ([]uint, error) { return f.active, nil }

type fakeSettings struct {
	prefs  map[uint][]domain.NotificationPreference
	locale map[uint]string
}

func (f *fakeSettings) ListPreferences(_ context.Context, id uint) ([]domain.NotificationPreference, error) {
	return f.prefs[id], nil
}
func (f *fakeSettings) SavePreferences(context.Context, uint, []domain.NotificationPreference) error {
	return nil
}
func (f *fakeSettings) Locale(_ context.Context, id uint) (string, error) {
	if l, ok := f.locale[id]; ok {
		return l, nil
	}
	return "en", nil
}
func (f *fakeSettings) SaveLocale(context.Context, uint, string) error { return nil }

type fakeInbox struct {
	mu        sync.Mutex
	drafts    []domain.NotificationDraft
	counts    map[string]int64
	countsErr error
	marked    int64
	deleteErr error
}

func (f *fakeInbox) Create(_ context.Context, d domain.NotificationDraft, _ int) (domain.NotificationRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drafts = append(f.drafts, d)
	return domain.NotificationRef{ID: uint(len(f.drafts)), CreatedAt: time.Unix(1700000000, 0).UTC()}, nil
}
func (f *fakeInbox) List(context.Context, uint, domain.NotificationFilter, int, int) ([]domain.Notification, int64, error) {
	return nil, 0, nil
}
func (f *fakeInbox) UnreadCounts(context.Context, uint) (map[string]int64, error) {
	return f.counts, f.countsErr
}
func (f *fakeInbox) MarkRead(context.Context, uint, []uint, bool, string) (int64, error) {
	return f.marked, nil
}
func (f *fakeInbox) Delete(context.Context, uint, uint) error { return f.deleteErr }

type fakeComments struct{ comment *domain.Comment }

func (f *fakeComments) Create(context.Context, uint, uint, string, []uint) (*domain.Comment, error) {
	return nil, nil
}
func (f *fakeComments) ListByTask(context.Context, uint, int, int) ([]domain.Comment, int64, error) {
	return nil, 0, nil
}
func (f *fakeComments) GetByID(context.Context, uint, uint) (*domain.Comment, error) {
	if f.comment == nil {
		return nil, domain.ErrNotFound
	}
	return f.comment, nil
}
func (f *fakeComments) Update(context.Context, uint, uint, *string, *[]uint) (*domain.Comment, error) {
	return nil, nil
}
func (f *fakeComments) Delete(context.Context, uint, uint) error { return nil }

type fakeChannelRepo struct {
	mu       sync.Mutex
	channels []domain.NotificationChannel
	tokens   map[string]uint
	created  map[uint]time.Time
	nextID   uint
}

func newFakeChannelRepo() *fakeChannelRepo {
	return &fakeChannelRepo{tokens: map[string]uint{}, created: map[uint]time.Time{}}
}

func (f *fakeChannelRepo) Create(_ context.Context, _ uint, kind, dest string, max int) (*domain.NotificationChannel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, c := range f.channels {
		if c.Kind == kind {
			count++
		}
		if c.Kind == kind && c.Destination == dest {
			return nil, domain.ErrChannelExists
		}
	}
	if count >= max {
		return nil, domain.ErrChannelLimitReached
	}
	f.nextID++
	ch := domain.NotificationChannel{ID: f.nextID, Kind: kind, Destination: dest}
	f.channels = append(f.channels, ch)
	return &ch, nil
}
func (f *fakeChannelRepo) List(context.Context, uint) ([]domain.NotificationChannel, error) {
	return f.channels, nil
}
func (f *fakeChannelRepo) Get(_ context.Context, _ uint, id uint) (*domain.NotificationChannel, error) {
	for i := range f.channels {
		if f.channels[i].ID == id {
			return &f.channels[i], nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeChannelRepo) Delete(context.Context, uint, uint) error { return nil }
func (f *fakeChannelRepo) VerifiedDestinations(_ context.Context, _ uint, kind string) ([]string, error) {
	var out []string
	for _, c := range f.channels {
		if c.Kind == kind && c.VerifiedAt != nil {
			out = append(out, c.Destination)
		}
	}
	return out, nil
}
func (f *fakeChannelRepo) SaveConfirmation(_ context.Context, id uint, hash string, _ time.Time) error {
	f.tokens[hash] = id
	f.created[id] = time.Now()
	return nil
}
func (f *fakeChannelRepo) DiscardConfirmation(_ context.Context, hash string) error {
	delete(f.tokens, hash)
	return nil
}
func (f *fakeChannelRepo) LastConfirmationAt(_ context.Context, id uint) (*time.Time, error) {
	if at, ok := f.created[id]; ok {
		return &at, nil
	}
	return nil, nil
}
func (f *fakeChannelRepo) Confirm(_ context.Context, hash string) error {
	id, ok := f.tokens[hash]
	if !ok {
		return domain.ErrInvalidToken
	}
	delete(f.tokens, hash)
	now := time.Now()
	for i := range f.channels {
		if f.channels[i].ID == id {
			f.channels[i].VerifiedAt = &now
		}
	}
	return nil
}

type allowPerms struct {
	denied          map[uint]bool
	deniedWorkspace map[uint]bool
}

func (a allowPerms) HasWorkspacePermission(_ context.Context, user, _ uint, _ string) (bool, error) {
	return !a.deniedWorkspace[user], nil
}
func (a allowPerms) HasSpacePermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}
func (a allowPerms) HasFolderPermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}
func (a allowPerms) HasListPermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}
func (a allowPerms) HasTaskPermission(_ context.Context, user, _ uint, _ string) (bool, error) {
	return !a.denied[user], nil
}

type notifyRig struct {
	dispatcher *NotificationDispatcher
	inbox      *fakeInbox
	mailbox    *mail.Memory
	watchers   *fakeWatchers
	settings   *fakeSettings
	channels   *fakeChannelRepo
	comments   *fakeComments
	throttle   *EmailThrottle
}

func newNotifyRig(t *testing.T, perms allowPerms, emailLimit int) *notifyRig {
	t.Helper()
	rig := &notifyRig{
		inbox: &fakeInbox{}, mailbox: mail.NewMemory(), watchers: &fakeWatchers{},
		settings: &fakeSettings{prefs: map[uint][]domain.NotificationPreference{}, locale: map[uint]string{}},
		channels: newFakeChannelRepo(), comments: &fakeComments{}, throttle: NewEmailThrottle(emailLimit, time.Hour),
	}
	source := &fakeSource{accounts: map[uint]domain.AccountMailTarget{
		1: {Email: "actor@example.test", Verified: true},
		2: {Email: "two@example.test", Verified: true},
		3: {Email: "three@example.test", Verified: false},
		4: {Email: "four@example.test", Verified: true},
	}}
	registry := NewChannelRegistry(
		NewInAppChannel(rig.inbox, 1000, nil),
		NewEmailChannel(source, rig.channels, rig.mailbox, "https://app.example.test", rig.throttle),
	)
	rig.dispatcher = NewNotificationDispatcher(NotificationDispatcherDeps{
		Source: source, Watchers: rig.watchers, Settings: rig.settings, Comments: rig.comments,
		Perms: perms, Channels: registry,
	})
	t.Cleanup(rig.dispatcher.Close)
	return rig
}

func (r *notifyRig) run(entries ...domain.ActivityEntry) {
	r.dispatcher.Enqueue(entries)
	r.dispatcher.Wait()
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func draftTypes(drafts []domain.NotificationDraft) map[uint]string {
	out := map[uint]string{}
	for _, d := range drafts {
		out[d.UserID] = d.Type
	}
	return out
}

func TestNotificationTypeRegistryDefaults(t *testing.T) {
	want := map[string][2]bool{
		domain.NotificationMention:       {true, true},
		domain.NotificationAssigned:      {true, true},
		domain.NotificationComment:       {true, false},
		domain.NotificationStatusChanged: {true, false},
		domain.NotificationDueDateChange: {true, false},
		domain.NotificationFormSubmitted: {true, false},
	}
	types := domain.NotificationTypes()
	if len(types) != len(want) {
		t.Fatalf("got %d types, want %d", len(types), len(want))
	}
	for _, typ := range types {
		w := want[typ.Key]
		if typ.DefaultEnabled(domain.ChannelKindInApp) != w[0] || typ.DefaultEnabled(domain.ChannelKindEmail) != w[1] || typ.DefaultEnabled(domain.ChannelKindWebhook) != w[1] {
			t.Errorf("%s defaults wrong", typ.Key)
		}
		if typ.DefaultEnabled("carrier-pigeon") {
			t.Errorf("%s must default to off for unknown channels", typ.Key)
		}
		for _, locale := range []string{"en", "ru"} {
			if !strings.Contains(typ.WordingFor(locale).Subject, "%[2]s") {
				t.Errorf("%s has no %s wording", typ.Key, locale)
			}
		}
		if typ.WordingFor("fr").Subject != typ.WordingFor("en").Subject {
			t.Errorf("%s does not fall back to English", typ.Key)
		}
	}
	if _, ok := domain.NotificationTypeByKey("nope"); ok {
		t.Fatal("unknown type resolved")
	}
}

func TestResolveRecipientsStrongestReasonWinsAndActorIsExcluded(t *testing.T) {
	plan := &notificationPlan{
		Actor: domain.UserActor(1), WatcherType: domain.NotificationComment,
		Direct: []directTarget{
			{2, domain.NotificationMention}, {3, domain.NotificationAssigned}, {3, domain.NotificationMention}, {1, domain.NotificationMention},
		},
	}
	got := resolveRecipients(plan, []uint{1, 2, 3, 4})
	want := map[uint]string{2: domain.NotificationMention, 3: domain.NotificationMention, 4: domain.NotificationComment}
	if len(got) != len(want) {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
	for id, typ := range want {
		if got[id] != typ {
			t.Errorf("user %d got %q, want %q", id, got[id], typ)
		}
	}
}

func TestEffectiveEnabledUsesStoredPreferenceOverDefault(t *testing.T) {
	typ, _ := domain.NotificationTypeByKey(domain.NotificationComment)
	prefs := map[[2]string]bool{{domain.NotificationComment, domain.ChannelKindEmail}: true, {domain.NotificationComment, domain.ChannelKindInApp}: false}
	if !effectiveEnabled(typ, domain.ChannelKindEmail, prefs) || effectiveEnabled(typ, domain.ChannelKindInApp, prefs) {
		t.Fatal("stored preferences must override defaults")
	}
	if !effectiveEnabled(typ, domain.ChannelKindInApp, nil) || effectiveEnabled(typ, domain.ChannelKindEmail, nil) {
		t.Fatal("defaults must apply without rows")
	}
}

func TestEmailThrottleWindow(t *testing.T) {
	th := NewEmailThrottle(2, time.Hour)
	now := time.Now()
	th.now = func() time.Time { return now }
	first, second, third := th.Allow(1), th.Allow(1), th.Allow(1)
	if !first || !second || third {
		t.Fatal("the third mail within the hour must be refused")
	}
	if !th.Allow(2) {
		t.Fatal("limits are per user")
	}
	now = now.Add(61 * time.Minute)
	if !th.Allow(1) {
		t.Fatal("mails are allowed again after the window")
	}
}

func TestEventMappingIgnoresUnknownActionsAndUnrelatedFields(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.watchers.active = []uint{2}
	rig.run(
		domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: "attachment.added", NewValue: raw(map[string]any{"name": "x"})},
		domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "title", NewValue: raw("new")},
		domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskMoved, Field: "list_id"},
	)
	if len(rig.inbox.drafts) != 0 || len(rig.mailbox.Messages()) != 0 {
		t.Fatal("unrelated activity must not notify anyone")
	}
}

func TestAssignmentNotifiesOnlyNewAssigneesAndAutoWatches(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.run(domain.ActivityEntry{
		TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "assignee_ids",
		OldValue: raw([]uint{2}), NewValue: raw([]uint{2, 4}),
	})
	got := draftTypes(rig.inbox.drafts)
	if len(got) != 1 || got[4] != domain.NotificationAssigned {
		t.Fatalf("drafts = %v, want only user 4 assigned", got)
	}
	if len(rig.mailbox.To("four@example.test")) != 1 {
		t.Fatal("assigned defaults to e-mail as well")
	}
	if len(rig.watchers.auto) != 1 || rig.watchers.auto[0] != 4 {
		t.Fatalf("auto-watch = %v", rig.watchers.auto)
	}
}

func TestStatusChangeGoesToWatchersWithoutEmailByDefault(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.watchers.active = []uint{1, 2, 4}
	rig.run(domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "status_id", NewValue: raw(7)})
	got := draftTypes(rig.inbox.drafts)
	if len(got) != 2 || got[2] != domain.NotificationStatusChanged || got[4] != domain.NotificationStatusChanged {
		t.Fatalf("drafts = %v", got)
	}
	if len(rig.mailbox.Messages()) != 0 {
		t.Fatal("status changes are in-app only by default")
	}
	var payload map[string]any
	_ = json.Unmarshal(rig.inbox.drafts[0].Payload, &payload)
	if payload["status_name"] != "Done" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestCommentMentionBeatsWatchAndMailEscapesInput(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.comments.comment = &domain.Comment{
		ID: 3, TaskID: 5, Author: domain.CommentUser{ID: 1}, Body: "hey <script>alert(1)</script>",
		Mentions: []domain.CommentUser{{ID: 2}},
	}
	rig.watchers.active = []uint{2, 4}
	rig.run(domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityCommentCreated, NewValue: raw(map[string]uint{"comment_id": 3})})
	got := draftTypes(rig.inbox.drafts)
	if got[2] != domain.NotificationMention || got[4] != domain.NotificationComment || len(got) != 2 {
		t.Fatalf("drafts = %v", got)
	}
	msgs := rig.mailbox.To("two@example.test")
	if len(msgs) != 1 {
		t.Fatalf("mentioned user got %d mails", len(msgs))
	}
	if strings.Contains(msgs[0].HTML, "<script>") || strings.Contains(msgs[0].HTML, "<b>login</b>") {
		t.Fatalf("html is not escaped: %s", msgs[0].HTML)
	}
	if !strings.Contains(msgs[0].Text, "https://app.example.test/tasks/5") || !strings.Contains(msgs[0].Text, "/settings/notifications") {
		t.Fatalf("text lacks links: %s", msgs[0].Text)
	}
	if len(rig.mailbox.To("four@example.test")) != 0 {
		t.Fatal("a plain comment does not mail by default")
	}
}

func TestUnreadableRecipientsAreSkipped(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{denied: map[uint]bool{2: true}}, 30)
	rig.watchers.active = []uint{2, 4}
	rig.run(domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "due_date", NewValue: raw("2030-01-01T10:00:00Z")})
	got := draftTypes(rig.inbox.drafts)
	if len(got) != 1 || got[4] != domain.NotificationDueDateChange {
		t.Fatalf("drafts = %v", got)
	}
}

func TestEmailNeedsConfirmedDestinationAndThrottles(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 2)
	assign := func(user uint) domain.ActivityEntry {
		return domain.ActivityEntry{
			TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "assignee_ids",
			OldValue: raw([]uint{}), NewValue: raw([]uint{user}),
		}
	}
	rig.run(assign(3))
	if len(rig.mailbox.Messages()) != 0 {
		t.Fatal("an unverified account address must not receive notifications")
	}
	if len(rig.inbox.drafts) != 1 {
		t.Fatal("the in-app entry is independent of the e-mail address")
	}
	rig.run(assign(2), assign(2), assign(2))
	if n := len(rig.mailbox.To("two@example.test")); n != 2 {
		t.Fatalf("mails = %d, want the hourly limit of 2", n)
	}
	if n := len(rig.inbox.drafts); n != 4 {
		t.Fatalf("inbox entries = %d, throttled mails keep their in-app entry", n)
	}
}

func TestExtraAddressReceivesMailOnlyWhenConfirmed(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	svc := NewNotificationChannelService(rig.channels, &fakeSource{}, NewChannelRegistry(
		NewEmailChannel(&fakeSource{}, rig.channels, rig.mailbox, "https://app.example.test", rig.throttle),
	))
	if _, err := svc.Add(context.Background(), 2, "email", "Extra@Example.test", "en"); err != nil {
		t.Fatal(err)
	}
	if len(rig.mailbox.To("extra@example.test")) != 1 {
		t.Fatal("the unconfirmed address must receive exactly its confirmation mail")
	}
	assign := domain.ActivityEntry{
		TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "assignee_ids", OldValue: raw([]uint{}), NewValue: raw([]uint{2}),
	}
	rig.run(assign)
	if len(rig.mailbox.To("extra@example.test")) != 1 {
		t.Fatal("notification reached an unconfirmed address")
	}
	confirmation := rig.mailbox.To("extra@example.test")[0]
	token := ""
	for _, f := range strings.Fields(confirmation.Text) {
		if i := strings.Index(f, "token="); i >= 0 {
			token = f[i+len("token="):]
		}
	}
	if err := svc.Confirm(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if err := svc.Confirm(context.Background(), token); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("a token must work once, got %v", err)
	}
	rig.run(assign)
	if len(rig.mailbox.To("extra@example.test")) != 2 || len(rig.mailbox.To("two@example.test")) != 2 {
		t.Fatalf("after confirmation both addresses are used: %d", len(rig.mailbox.Messages()))
	}
}

func TestChannelServiceValidationLimitsAndCooldown(t *testing.T) {
	repo := newFakeChannelRepo()
	mailbox := mail.NewMemory()
	registry := NewChannelRegistry(
		NewInAppChannel(&fakeInbox{}, 10, nil),
		NewEmailChannel(&fakeSource{}, repo, mailbox, "https://app.example.test", NewEmailThrottle(1, time.Hour)),
	)
	svc := NewNotificationChannelService(repo, &fakeSource{}, registry)
	ctx := context.Background()

	if _, err := svc.Add(ctx, 1, "sms", "x", "en"); !errors.Is(err, domain.ErrUnknownChannelKind) {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := svc.Add(ctx, 1, "in_app", "x", "en"); !errors.Is(err, domain.ErrChannelNeedsNoDestination) {
		t.Fatalf("in_app: %v", err)
	}
	for _, bad := range []string{"nope", "a@b", "Name <a@b.test>", "a b@c.test", ""} {
		if _, err := svc.Add(ctx, 1, "email", bad, "en"); !errors.Is(err, domain.ErrInvalidDestination) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	view, err := svc.Add(ctx, 1, "email", "a@b.test", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if msg := mailbox.To("a@b.test")[0]; !strings.Contains(msg.Subject, "Подтвердите") {
		t.Fatalf("not localized: %q", msg.Subject)
	}
	if _, err := svc.Add(ctx, 1, "email", "A@B.test", "en"); !errors.Is(err, domain.ErrChannelExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := svc.Resend(ctx, 1, *view.ID, "en"); !errors.Is(err, domain.ErrConfirmationTooSoon) {
		t.Fatalf("resend cooldown: %v", err)
	}
	svc.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	repo.created[*view.ID] = time.Now().Add(-2 * time.Minute)
	if err := svc.Resend(ctx, 1, *view.ID, "en"); err != nil {
		t.Fatalf("resend after cooldown: %v", err)
	}
	for i := 0; i < domain.MaxChannelsPerUserKind-1; i++ {
		if _, err := svc.Add(ctx, 1, "email", string(rune('c'+i))+"@b.test", "en"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Add(ctx, 1, "email", "z@b.test", "en"); !errors.Is(err, domain.ErrChannelLimitReached) {
		t.Fatalf("limit: %v", err)
	}
}

func TestDispatcherSurvivesPanicsAndDrainsOnClose(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.dispatcher.mappers["boom"] = func(context.Context, *NotificationDispatcher, domain.ActivityEntry) (*notificationPlan, error) {
		panic("boom")
	}
	rig.dispatcher.Enqueue([]domain.ActivityEntry{{TaskID: 1, Actor: domain.UserActor(1), Action: "boom"}})
	rig.watchers.active = []uint{2}
	rig.run(domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "status_id", NewValue: raw(7)})
	if len(rig.inbox.drafts) != 1 {
		t.Fatal("the worker must keep running after a panic")
	}
	rig.dispatcher.Enqueue([]domain.ActivityEntry{{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "status_id", NewValue: raw(8)}})
	rig.dispatcher.Close()
	if len(rig.inbox.drafts) != 2 {
		t.Fatal("Close must drain queued events")
	}
	rig.dispatcher.Enqueue([]domain.ActivityEntry{{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "status_id", NewValue: raw(9)}})
	rig.dispatcher.Wait()
}

func TestDispatcherShutdownHonorsContextAndCancelsCurrentBatch(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	started := make(chan struct{})
	rig.dispatcher.mappers["block"] = func(ctx context.Context, _ *NotificationDispatcher, _ domain.ActivityEntry) (*notificationPlan, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	rig.dispatcher.Enqueue([]domain.ActivityEntry{{Action: "block"}})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := rig.dispatcher.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	select {
	case <-rig.dispatcher.done:
	case <-time.After(time.Second):
		t.Fatal("notification worker did not stop after cancellation")
	}
}

func TestDispatcherQueueIsRaceFree(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 1000)
	rig.watchers.active = []uint{2, 4}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				rig.dispatcher.Enqueue([]domain.ActivityEntry{{
					TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "status_id", NewValue: raw(7),
				}})
			}
		}()
	}
	wg.Wait()
	rig.dispatcher.Close()
	rig.inbox.mu.Lock()
	defer rig.inbox.mu.Unlock()
	if len(rig.inbox.drafts) != 8*25*2 {
		t.Fatalf("drafts = %d, want %d", len(rig.inbox.drafts), 8*25*2)
	}
}

func TestNotifyingRecorderForwardsAndDispatches(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.watchers.active = []uint{2}
	var forwarded []domain.ActivityEntry
	next := recorderFunc(func(_ context.Context, e ...domain.ActivityEntry) { forwarded = append(forwarded, e...) })
	recorder := NewNotifyingRecorder(next, rig.dispatcher)
	recorder.Record(context.Background(), domain.ActivityEntry{TaskID: 5, Actor: domain.UserActor(1), Action: domain.ActivityTaskUpdated, Field: "status_id", NewValue: raw(7)})
	rig.dispatcher.Wait()
	if len(forwarded) != 1 || len(rig.inbox.drafts) != 1 {
		t.Fatalf("forwarded=%d drafts=%d", len(forwarded), len(rig.inbox.drafts))
	}
}

type recorderFunc func(ctx context.Context, entries ...domain.ActivityEntry)

func (f recorderFunc) Record(ctx context.Context, entries ...domain.ActivityEntry) {
	f(ctx, entries...)
}

func TestPreferencesMatrixReportsUsability(t *testing.T) {
	settings := &fakeSettings{prefs: map[uint][]domain.NotificationPreference{
		3: {{Type: domain.NotificationComment, Channel: domain.ChannelKindEmail, Enabled: true}},
	}}
	repo := newFakeChannelRepo()
	source := &fakeSource{accounts: map[uint]domain.AccountMailTarget{
		2: {Email: "a@b.test", Verified: true}, 3: {Email: "c@d.test"},
	}}
	registry := NewChannelRegistry(NewInAppChannel(&fakeInbox{}, 10, nil), NewEmailChannel(source, repo, mail.NewMemory(), "x", NewEmailThrottle(1, time.Hour)))
	svc := NewNotificationService(&fakeInbox{}, settings, &fakeWatchers{}, registry)

	matrix, err := svc.Preferences(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix) != len(domain.NotificationTypes())*2 {
		t.Fatalf("matrix has %d cells", len(matrix))
	}
	for _, cell := range matrix {
		if cell.Channel == domain.ChannelKindEmail && cell.Usable {
			t.Fatal("e-mail is not usable while the account address is unverified")
		}
		if cell.Type == domain.NotificationComment && cell.Channel == domain.ChannelKindEmail && (!cell.Enabled || cell.Default) {
			t.Fatalf("explicit preference not reflected: %+v", cell)
		}
	}
	if _, err := svc.SavePreferences(context.Background(), 3, []domain.NotificationPreference{{Type: "x", Channel: "email"}}); !errors.Is(err, domain.ErrUnknownNotificationType) {
		t.Fatalf("unknown type: %v", err)
	}
	if _, err := svc.SavePreferences(context.Background(), 3, []domain.NotificationPreference{{Type: "comment", Channel: "fax"}}); !errors.Is(err, domain.ErrUnknownChannelKind) {
		t.Fatalf("unknown channel: %v", err)
	}
}
