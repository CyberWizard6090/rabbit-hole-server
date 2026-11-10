package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/handler"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/ratelimit"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/repository"
	"rabbit-hole-server/internal/safehttp"
	"rabbit-hole-server/internal/service"
	"rabbit-hole-server/internal/storage"
)

type Container struct {
	AuthService         *service.AuthService
	AuthHandler         *handler.AuthHandler
	SessionHandler      *handler.SessionHandler
	AccountEmailHandler *handler.AccountEmailHandler
	EmailChangeHandler  *handler.EmailChangeHandler
	TaskHandler         *handler.TaskHandler
	CommentHandler      *handler.CommentHandler
	ActivityHandler     *handler.ActivityHandler
	ChecklistHandler    *handler.ChecklistHandler
	FileHandler         *handler.FileHandler
	NotificationHandler *handler.NotificationHandler
	Notifications       *service.NotificationDispatcher
	Workers             []*service.OutboxWorker
	RateLimiter         ratelimit.Limiter
	PurgeRateLimits     func(context.Context) error
	PurgeData           func(context.Context) (domain.RetentionResult, error)
	SyncAccess          func(context.Context) (domain.AccessSyncResult, error)
	RetentionInterval   time.Duration
	UserHandler         *handler.UserHandler

	WorkspaceHandler *handler.WorkspaceHandler
	FolderHandler    *handler.FolderHandler
	ListHandler      *handler.ListHandler

	ContactHandler      *handler.ContactHandler
	SearchHandler       *handler.SearchHandler
	SpaceHandler        *handler.SpaceHandler
	StatusHandler       *handler.StatusHandler
	TagHandler          *handler.TagHandler
	CustomFieldHandler  *handler.CustomFieldHandler
	FormHandler         *handler.FormHandler
	FormUploadHandler   *handler.FormUploadHandler
	FormUploads         service.FormUploadService
	FormTaskHandler     *handler.FormTaskHandler
	TaskTemplateHandler *handler.TaskTemplateHandler
	TimeHandler         *handler.TimeHandler
	TaskRelationHandler *handler.TaskRelationHandler
	SavedViewHandler    *handler.SavedViewHandler

	PermissionChecker service.PermissionChecker

	Ready           func(ctx context.Context) error
	Realtime        *realtime.Hub
	RealtimeHandler *handler.RealtimeHandler
}

func NewContainer(db *gorm.DB, cfg *config.Config) (*Container, error) {
	transport, err := newMailTransport(cfg)
	if err != nil {
		return nil, err
	}
	return NewContainerWithMailer(db, cfg, transport)
}

func newMailTransport(cfg *config.Config) (mail.Sender, error) {
	if cfg.Mail.Driver != "smtp" {
		return mail.NewLog(slog.Default()).WithBody(cfg.Environment == "development"), nil
	}
	smtp, err := mail.NewSMTP(mail.SMTPOptions{
		Host: cfg.Mail.SMTPHost, Port: cfg.Mail.SMTPPort, Username: cfg.Mail.SMTPUsername, Password: cfg.Mail.SMTPPassword,
		Security: cfg.Mail.SMTPSecurity, From: cfg.Mail.From,
	})
	if err != nil {
		return nil, fmt.Errorf("configure the mail sender: %w", err)
	}
	return smtp, nil
}

type ContainerOption func(*containerOptions)

type containerOptions struct {
	remoteFetcher service.RemoteFetcher
	webhookClient service.WebhookClient
}

func WithWebhookClient(client service.WebhookClient) ContainerOption {
	return func(o *containerOptions) { o.webhookClient = client }
}

func WithRemoteFetcher(fetcher service.RemoteFetcher) ContainerOption {
	return func(o *containerOptions) { o.remoteFetcher = fetcher }
}

func NewContainerWithMailer(db *gorm.DB, cfg *config.Config, transport mail.Sender, opts ...ContainerOption) (*Container, error) {
	options := containerOptions{
		remoteFetcher: safehttp.New(),
		webhookClient: safehttp.New(
			safehttp.WithTimeout(webhookRequestTimeout), safehttp.WithMaxRedirects(0), safehttp.WithUserAgent(webhookUserAgent),
		),
	}
	for _, opt := range opts {
		opt(&options)
	}
	hub := realtime.NewHub(realtime.DefaultMaxConnectionsPerUser, slog.Default())
	secretBox, err := service.NewSecretBox(cfg.JWT.Secret)
	if err != nil {
		return nil, fmt.Errorf("create secret box: %w", err)
	}
	outboxRepo := repository.NewOutboxRepository(db)
	var limiter ratelimit.Limiter = ratelimit.NewMemory()
	var lockout ratelimit.Lockout
	lockoutConfig := ratelimit.BackoffConfig{
		Threshold: cfg.Auth.LoginFailureLimit, Base: cfg.Auth.LoginLockBase, Max: cfg.Auth.LoginLockMax,
	}
	var purgeLimits func(context.Context) error
	if cfg.RateLimit.Store == "memory" {
		lockoutConfig.MaxKeys = ratelimit.DefaultMaxKeys
		lockout = ratelimit.NewMemoryLockout(lockoutConfig)
	} else {
		store := repository.NewRateLimiter(db)
		limiter = store
		lockout = repository.NewLockoutStore(db, lockoutConfig)
		purgeLimits = func(ctx context.Context) error { return store.PurgeExpired(ctx, time.Hour) }
	}
	transactions := database.NewTransactionRunner(db)
	eventWorker := service.NewOutboxWorker(outboxRepo, transactions, 1, slog.Default())
	deliveryWorker := service.NewOutboxWorker(outboxRepo, nil, deliveryWorkers, slog.Default())
	sender := service.NewOutboxMailer(outboxRepo, secretBox, transport)
	sender.AttachWorker(deliveryWorker)
	userRepo := repository.NewUserRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	contactRepo := repository.NewContactRepository(db)
	spaceRepo := repository.NewSpaceRepository(db)
	statusRepo := repository.NewStatusRepository(db)
	tagRepo := repository.NewTagRepository(db)
	folderRepo := repository.NewFolderRepository(db)
	workspaceRepo := repository.NewWorkspaceRepository(db)

	accountEmail := service.NewAccountEmailService(userRepo, repository.NewEmailTokenRepository(db), sender, service.AccountEmailConfig{
		FrontendURL: cfg.Mail.FrontendURL, VerificationTTL: cfg.Mail.VerificationTTL, ResetTTL: cfg.Mail.ResetTTL,
	}).WithConnectionRevoker(hub)
	emailChange := service.NewEmailChangeService(userRepo, repository.NewEmailChangeRepository(db), sender, cfg.Mail.FrontendURL)
	authServ := service.NewAuthService(userRepo, cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL).
		WithVerificationSender(accountEmail).
		WithRequireVerifiedEmail(cfg.Mail.RequireVerifiedEmail).
		WithConnectionRevoker(hub).
		WithLoginBackoff(lockout).
		WithSecurityNotifier(accountEmail)
	authHandler := handler.NewAuthHandler(authServ, cfg.SecureCookies())
	service.ConfigurePasswordHashing(cfg.Auth.HashSlots(), cfg.Auth.HashWait)
	spaceServ := service.NewSpaceService(spaceRepo)
	statusServ := service.NewStatusService(statusRepo, spaceRepo)
	tagService := service.NewTagService(tagRepo)
	customFieldService := service.NewCustomFieldService(
		repository.NewCustomFieldRepository(db),
		repository.NewTaskCustomFieldValueRepository(db),
	)
	formRepo := repository.NewFormRepository(db)
	templateRepo := repository.NewTaskTemplateRepository(db)
	templateService := service.NewTaskTemplateService(templateRepo, customFieldService, spaceRepo, tagRepo, workspaceRepo)
	formTaskConfigService := service.NewFormTaskConfigurationService(
		formRepo, spaceRepo, templateRepo, customFieldService, tagRepo, workspaceRepo,
	)
	formUploadRepo := repository.NewFormUploadRepository(db)
	formService := service.NewFormService(formRepo).
		WithSubmissionLimit(limiter, cfg.Forms.SubmissionsPerHour, time.Hour).
		WithTransactions(database.NewTransactionRunner(db)).
		WithSettingsPolicy(cfg.Environment == "development", cfg.Storage.PublicBaseURL).
		WithUploads(formUploadRepo)
	keyDerivation := hmac.New(sha256.New, []byte(cfg.JWT.Secret))
	_, _ = keyDerivation.Write([]byte("rabbit-hole-server/task-cursor-signing/v1"))
	cursorCodec, err := pagination.NewCodec(keyDerivation.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create task cursor codec: %w", err)
	}
	activityService := service.NewActivityService(repository.NewActivityRepository(db))
	permissionChecker := repository.NewPermissionChecker(db)
	tickets := realtime.NewTicketStore(realtime.DefaultTicketTTL)
	notifications, err := newNotifications(db, cfg, sender, permissionChecker, hub, options.webhookClient, secretBox, outboxRepo, deliveryWorker, limiter)
	if err != nil {
		return nil, err
	}
	notifications.dispatcher.AttachWorker(eventWorker)
	notifications.dispatcher.DrainAlso(deliveryWorker)
	activityRecorder := service.NewNotifyingRecorder(activityService, notifications.dispatcher, transactions)
	taskService := service.NewTaskService(
		taskRepo, spaceRepo, tagService, cursorCodec, activityRecorder, customFieldService, database.NewTransactionRunner(db),
	)
	taskProcessor := service.NewSubmissionTaskProcessor(
		formRepo, taskService, templateRepo, customFieldService, formUploadRepo, activityRecorder,
	)
	templateApplier := service.NewTaskTemplateApplier(database.NewTransactionRunner(db), templateRepo, spaceRepo, taskService)
	formService.WithTaskIntegration(formTaskConfigService, taskProcessor).
		WithSubmissionNotifier(notifications.dispatcher)
	userService := service.NewUserService(userRepo)
	contactService := service.NewContactService(contactRepo)
	folderService := service.NewFolderService(folderRepo)

	commentService := service.NewCommentService(repository.NewCommentRepository(db), permissionChecker, activityRecorder).
		WithTransactions(transactions)

	store, err := storage.New(context.Background(), cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("open file storage: %w", err)
	}
	fileService := service.NewFileService(
		repository.NewFileRepository(db), store, permissionChecker,
		service.FileLimits{
			Attachment: cfg.Storage.MaxAttachmentBytes, Image: cfg.Storage.MaxImageBytes, Avatar: cfg.Storage.MaxAvatarBytes,
		},
		cfg.Storage.PublicBaseURL,
		activityRecorder,
		service.RehostConfig{Fetcher: options.remoteFetcher, MaxBytes: cfg.Storage.RehostLimit()},
	).WithTransactions(transactions).WithWorkspaceAudit(repository.NewWorkspaceAuditRepository(db))
	authServ.WithAvatarRemover(fileService)
	workspaceAudit := repository.NewWorkspaceAuditRepository(db)
	workspaceService := service.NewWorkspaceServiceWithAvatars(workspaceRepo, hub, permissionChecker, fileService).
		WithAudit(workspaceAudit, transactions)
	formUploadService := service.NewFormUploadService(
		formRepo, formUploadRepo, store, cfg.Forms.UploadMaxBytes, cfg.Forms.UploadTTL,
	)

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database handle for readiness checks: %w", err)
	}

	return &Container{
		AuthService:         authServ,
		AuthHandler:         authHandler,
		SessionHandler:      handler.NewSessionHandler(authHandler),
		AccountEmailHandler: handler.NewAccountEmailHandler(accountEmail),
		EmailChangeHandler:  handler.NewEmailChangeHandler(emailChange),
		ActivityHandler:     handler.NewActivityHandler(activityService),
		ChecklistHandler:    handler.NewChecklistHandler(service.NewChecklistService(repository.NewChecklistRepository(db), activityRecorder).WithTransactions(transactions)),
		FileHandler:         handler.NewFileHandler(fileService),
		NotificationHandler: notifications.handler,
		Notifications:       notifications.dispatcher,
		Workers:             []*service.OutboxWorker{eventWorker, deliveryWorker},
		RateLimiter:         limiter,
		PurgeRateLimits:     purgeLimits,
		SyncAccess:          repository.NewAccessSyncRepository(db).SyncDefaultRoles,
		PurgeData: func(ctx context.Context) (domain.RetentionResult, error) {
			return repository.NewRetentionRepository(db).Purge(ctx, domain.RetentionPolicy{
				OutboxDone: cfg.Retention.OutboxDone, OutboxDead: cfg.Retention.OutboxDead, ExpiredTokens: cfg.Retention.ExpiredTokens,
			})
		},
		RetentionInterval: cfg.Retention.Interval,
		CommentHandler:    handler.NewCommentHandler(commentService, taskService).WithPublisher(hub),
		TaskHandler:       handler.NewTaskHandler(taskService, cursorCodec).WithPublisher(hub).WithPermissionChecker(permissionChecker),
		UserHandler:       handler.NewUserHandler(userService).WithPendingEmail(emailChange),

		ContactHandler:     handler.NewContactHandler(contactService),
		SearchHandler:      handler.NewSearchHandler(service.NewSearchService(repository.NewSearchRepository(db))),
		SpaceHandler:       handler.NewSpaceHandler(spaceServ),
		StatusHandler:      handler.NewStatusHandler(statusServ),
		TagHandler:         handler.NewTagHandler(tagService),
		CustomFieldHandler: handler.NewCustomFieldHandler(customFieldService),
		FormHandler:        handler.NewFormHandler(formService),
		FormUploadHandler:  handler.NewFormUploadHandler(formUploadService),
		FormUploads:        formUploadService,
		FormTaskHandler: handler.NewFormTaskHandler(
			formTaskConfigService, formRepo, taskProcessor, taskService, permissionChecker,
		),
		TaskTemplateHandler: handler.NewTaskTemplateHandler(
			templateService, permissionChecker, templateApplier,
		),
		TaskRelationHandler: handler.NewTaskRelationHandler(service.NewTaskRelationService(repository.NewTaskRelationRepository(db), activityRecorder, transactions)),
		TimeHandler:         handler.NewTimeHandler(service.NewTimeService(repository.NewTimeEntryRepository(db)), taskService, permissionChecker).WithPublisher(hub),
		SavedViewHandler:    handler.NewSavedViewHandler(service.NewSavedViewService(repository.NewSavedViewRepository(db), permissionChecker)),
		WorkspaceHandler:    handler.NewWorkspaceHandler(workspaceService),
		FolderHandler:       handler.NewFolderHandler(folderService),
		ListHandler:         handler.NewListHandler(spaceServ, folderService),
		PermissionChecker:   permissionChecker,
		Ready:               sqlDB.PingContext,

		Realtime:        hub,
		RealtimeHandler: handler.NewRealtimeHandler(hub, tickets, permissionChecker, cfg.AllowedOrigins).WithTokenVersions(authServ),
	}, nil
}

type notificationParts struct {
	dispatcher *service.NotificationDispatcher
	handler    *handler.NotificationHandler
}

const deliveryWorkers = 4

const (
	webhookRequestTimeout = 5 * time.Second
	webhookUserAgent      = "RabbitHole-Webhook/1.0"
)

func newNotifications(
	db *gorm.DB, cfg *config.Config, sender mail.Sender, permissions service.PermissionChecker,
	publisher service.NotificationPublisher, webhookClient service.WebhookClient,
	secretBox *service.SecretBox, outbox domain.OutboxRepository, deliveryWorker *service.OutboxWorker,
	limiter ratelimit.Limiter,
) (notificationParts, error) {
	inbox := repository.NewNotificationRepository(db)
	source := repository.NewNotificationSourceRepository(db)
	settings := repository.NewNotificationSettingsRepository(db)
	watchers := repository.NewWatcherRepository(db)
	channelRepo := repository.NewNotificationChannelRepository(db)

	throttle := service.NewEmailThrottle(limiter, cfg.Notifications.EmailPerHour, time.Hour)
	registry := service.NewChannelRegistry(
		service.NewInAppChannel(inbox, cfg.Notifications.MaxPerUser, publisher),
		service.NewEmailChannel(source, channelRepo, sender, cfg.Mail.FrontendURL, throttle),
		service.NewWebhookChannel(service.WebhookConfig{
			Client: webhookClient, Repo: repository.NewWebhookChannelRepository(db), Channels: channelRepo, Box: secretBox,
			Outbox:      outbox,
			FrontendURL: cfg.Mail.FrontendURL, AllowHTTP: cfg.Environment == "development",
			MaxFailures: cfg.Notifications.WebhookMaxFailures,
		}),
	)
	dispatcher := service.NewNotificationDispatcher(service.NotificationDispatcherDeps{
		Source: source, Watchers: watchers, Settings: settings, Comments: repository.NewCommentRepository(db),
		Perms: permissions, Channels: registry, Outbox: outbox, Logger: slog.Default(),
	})
	if webhook, ok := registry.Get(domain.ChannelKindWebhook); ok {
		if attachable, ok := webhook.(interface{ AttachWorker(*service.OutboxWorker) }); ok {
			attachable.AttachWorker(deliveryWorker)
		}
	}
	return notificationParts{
		dispatcher: dispatcher,
		handler: handler.NewNotificationHandler(
			service.NewNotificationService(inbox, settings, watchers, registry).WithPublisher(publisher),
			service.NewNotificationChannelService(channelRepo, source, registry),
		),
	}, nil
}
