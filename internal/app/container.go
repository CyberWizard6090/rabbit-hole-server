package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/handler"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/repository"
	"rabbit-hole-server/internal/safehttp"
	"rabbit-hole-server/internal/service"
	"rabbit-hole-server/internal/storage"
)

type Container struct {
	AuthService         *service.AuthService
	AuthHandler         *handler.AuthHandler
	AccountEmailHandler *handler.AccountEmailHandler
	EmailChangeHandler  *handler.EmailChangeHandler
	CloseMail           func(context.Context) error
	TaskHandler         *handler.TaskHandler
	CommentHandler      *handler.CommentHandler
	ActivityHandler     *handler.ActivityHandler
	ChecklistHandler    *handler.ChecklistHandler
	FileHandler         *handler.FileHandler
	NotificationHandler *handler.NotificationHandler
	Notifications       *service.NotificationDispatcher
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
	FormTaskHandler     *handler.FormTaskHandler
	TaskTemplateHandler *handler.TaskTemplateHandler

	PermissionChecker service.PermissionChecker

	Ready           func(ctx context.Context) error
	Realtime        *realtime.Hub
	RealtimeHandler *handler.RealtimeHandler
}

func NewContainer(db *gorm.DB, cfg *config.Config) (*Container, error) {
	sender, closeMail, err := newMailSender(cfg.Mail)
	if err != nil {
		return nil, err
	}
	container, err := NewContainerWithMailer(db, cfg, sender)
	if err != nil {
		if closeErr := closeMail(context.Background()); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close mail sender: %w", closeErr))
		}
		return nil, err
	}
	container.CloseMail = closeMail
	return container, nil
}

func newMailSender(cfg config.MailConfig) (mail.Sender, func(context.Context) error, error) {
	if cfg.Driver != "smtp" {
		return mail.NewLog(slog.Default()), func(context.Context) error { return nil }, nil
	}
	smtp, err := mail.NewSMTP(mail.SMTPOptions{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		Security: cfg.SMTPSecurity, From: cfg.From,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure the mail sender: %w", err)
	}
	async := mail.NewAsync(smtp, slog.Default())
	return async, async.Shutdown, nil
}

type ContainerOption func(*containerOptions)

type containerOptions struct {
	remoteFetcher service.RemoteFetcher
}

func WithRemoteFetcher(fetcher service.RemoteFetcher) ContainerOption {
	return func(o *containerOptions) { o.remoteFetcher = fetcher }
}

func NewContainerWithMailer(db *gorm.DB, cfg *config.Config, sender mail.Sender, opts ...ContainerOption) (*Container, error) {
	options := containerOptions{remoteFetcher: safehttp.New()}
	for _, opt := range opts {
		opt(&options)
	}
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
	})
	emailChange := service.NewEmailChangeService(userRepo, repository.NewEmailChangeRepository(db), sender, cfg.Mail.FrontendURL)
	authServ := service.NewAuthService(userRepo, cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL).
		WithVerificationSender(accountEmail).
		WithRequireVerifiedEmail(cfg.Mail.RequireVerifiedEmail)
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
	formService := service.NewFormService(formRepo).WithSubmissionLimit(cfg.Forms.SubmissionsPerHour, time.Hour)
	keyDerivation := hmac.New(sha256.New, []byte(cfg.JWT.Secret))
	_, _ = keyDerivation.Write([]byte("rabbit-hole-server/task-cursor-signing/v1"))
	cursorCodec, err := pagination.NewCodec(keyDerivation.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create task cursor codec: %w", err)
	}
	activityService := service.NewActivityService(repository.NewActivityRepository(db))
	permissionChecker := repository.NewPermissionChecker(db)
	hub := realtime.NewHub(realtime.DefaultMaxConnectionsPerUser, slog.Default())
	tickets := realtime.NewTicketStore(realtime.DefaultTicketTTL)
	notifications := newNotifications(db, cfg, sender, permissionChecker, hub)
	activityRecorder := service.NewNotifyingRecorder(activityService, notifications.dispatcher)
	taskService := service.NewTaskService(taskRepo, spaceRepo, tagService, cursorCodec, activityRecorder)
	taskProcessor := service.NewSubmissionTaskProcessor(formRepo, taskService, templateRepo, customFieldService)
	templateApplier := service.NewTaskTemplateApplier(database.NewTransactionRunner(db), templateRepo, spaceRepo, taskService, customFieldService)
	formService.WithTaskIntegration(formTaskConfigService, taskProcessor)
	userService := service.NewUserService(userRepo)
	contactService := service.NewContactService(contactRepo)
	folderService := service.NewFolderService(folderRepo)
	workspaceService := service.NewWorkspaceService(workspaceRepo)

	commentService := service.NewCommentService(repository.NewCommentRepository(db), permissionChecker, activityRecorder)

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
	)
	authServ.WithAvatarRemover(fileService)

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database handle for readiness checks: %w", err)
	}

	return &Container{
		AuthService:         authServ,
		AuthHandler:         handler.NewAuthHandler(authServ, cfg.SecureCookies()),
		AccountEmailHandler: handler.NewAccountEmailHandler(accountEmail),
		EmailChangeHandler:  handler.NewEmailChangeHandler(emailChange),
		ActivityHandler:     handler.NewActivityHandler(activityService),
		ChecklistHandler:    handler.NewChecklistHandler(service.NewChecklistService(repository.NewChecklistRepository(db), activityRecorder)),
		FileHandler:         handler.NewFileHandler(fileService),
		NotificationHandler: notifications.handler,
		Notifications:       notifications.dispatcher,
		CommentHandler:      handler.NewCommentHandler(commentService, taskService).WithPublisher(hub),
		TaskHandler:         handler.NewTaskHandler(taskService, cursorCodec).WithPublisher(hub).WithPermissionChecker(permissionChecker),
		UserHandler:         handler.NewUserHandler(userService).WithPendingEmail(emailChange),

		ContactHandler:     handler.NewContactHandler(contactService),
		SearchHandler:      handler.NewSearchHandler(service.NewSearchService(repository.NewSearchRepository(db))),
		SpaceHandler:       handler.NewSpaceHandler(spaceServ),
		StatusHandler:      handler.NewStatusHandler(statusServ),
		TagHandler:         handler.NewTagHandler(tagService),
		CustomFieldHandler: handler.NewCustomFieldHandler(customFieldService),
		FormHandler:        handler.NewFormHandler(formService),
		FormTaskHandler: handler.NewFormTaskHandler(
			formTaskConfigService, formRepo, taskProcessor, taskService, permissionChecker,
		),
		TaskTemplateHandler: handler.NewTaskTemplateHandler(
			templateService, permissionChecker, templateApplier,
		),
		WorkspaceHandler:  handler.NewWorkspaceHandler(workspaceService),
		FolderHandler:     handler.NewFolderHandler(folderService),
		ListHandler:       handler.NewListHandler(spaceServ, folderService),
		PermissionChecker: permissionChecker,
		Ready:             sqlDB.PingContext,

		Realtime:        hub,
		RealtimeHandler: handler.NewRealtimeHandler(hub, tickets, permissionChecker, cfg.AllowedOrigins),
	}, nil
}

type notificationParts struct {
	dispatcher *service.NotificationDispatcher
	handler    *handler.NotificationHandler
}

func newNotifications(db *gorm.DB, cfg *config.Config, sender mail.Sender, permissions service.PermissionChecker, publisher service.NotificationPublisher) notificationParts {
	inbox := repository.NewNotificationRepository(db)
	source := repository.NewNotificationSourceRepository(db)
	settings := repository.NewNotificationSettingsRepository(db)
	watchers := repository.NewWatcherRepository(db)
	channelRepo := repository.NewNotificationChannelRepository(db)

	throttle := service.NewEmailThrottle(cfg.Notifications.EmailPerHour, time.Hour)
	registry := service.NewChannelRegistry(
		service.NewInAppChannel(inbox, cfg.Notifications.MaxPerUser, publisher),
		service.NewEmailChannel(source, channelRepo, sender, cfg.Mail.FrontendURL, throttle),
	)
	dispatcher := service.NewNotificationDispatcher(service.NotificationDispatcherDeps{
		Source: source, Watchers: watchers, Settings: settings, Comments: repository.NewCommentRepository(db),
		Perms: permissions, Channels: registry, Logger: slog.Default(),
	})
	return notificationParts{
		dispatcher: dispatcher,
		handler: handler.NewNotificationHandler(
			service.NewNotificationService(inbox, settings, watchers, registry).WithPublisher(publisher),
			service.NewNotificationChannelService(channelRepo, source, registry),
		),
	}
}
