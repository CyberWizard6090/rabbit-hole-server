package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/handler"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/repository"
	"rabbit-hole-server/internal/service"
	"rabbit-hole-server/internal/storage"
)

type Container struct {
	AuthService         *service.AuthService
	AuthHandler         *handler.AuthHandler
	AccountEmailHandler *handler.AccountEmailHandler
	CloseMail           func()
	TaskHandler         *handler.TaskHandler
	CommentHandler      *handler.CommentHandler
	ActivityHandler     *handler.ActivityHandler
	ChecklistHandler    *handler.ChecklistHandler
	FileHandler         *handler.FileHandler
	UserHandler         *handler.UserHandler

	WorkspaceHandler *handler.WorkspaceHandler
	FolderHandler    *handler.FolderHandler
	ListHandler      *handler.ListHandler

	ContactHandler *handler.ContactHandler
	SearchHandler  *handler.SearchHandler
	SpaceHandler   *handler.SpaceHandler
	StatusHandler  *handler.StatusHandler
	TagHandler     *handler.TagHandler

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
		closeMail()
		return nil, err
	}
	container.CloseMail = closeMail
	return container, nil
}

func newMailSender(cfg config.MailConfig) (mail.Sender, func(), error) {
	if cfg.Driver != "smtp" {
		return mail.NewLog(slog.Default()), func() {}, nil
	}
	smtp, err := mail.NewSMTP(mail.SMTPOptions{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		Security: cfg.SMTPSecurity, From: cfg.From,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure the mail sender: %w", err)
	}
	async := mail.NewAsync(smtp, slog.Default())
	return async, async.Close, nil
}

func NewContainerWithMailer(db *gorm.DB, cfg *config.Config, sender mail.Sender) (*Container, error) {
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
	authServ := service.NewAuthService(userRepo, cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL).
		WithVerificationSender(accountEmail).
		WithRequireVerifiedEmail(cfg.Mail.RequireVerifiedEmail)
	spaceServ := service.NewSpaceService(spaceRepo)
	statusServ := service.NewStatusService(statusRepo, spaceRepo)
	tagService := service.NewTagService(tagRepo)
	keyDerivation := hmac.New(sha256.New, []byte(cfg.JWT.Secret))
	_, _ = keyDerivation.Write([]byte("rabbit-hole-server/task-cursor-signing/v1"))
	cursorCodec, err := pagination.NewCodec(keyDerivation.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create task cursor codec: %w", err)
	}
	activityService := service.NewActivityService(repository.NewActivityRepository(db))
	taskService := service.NewTaskService(taskRepo, spaceRepo, tagService, cursorCodec, activityService)
	userService := service.NewUserService(userRepo)
	contactService := service.NewContactService(contactRepo)
	folderService := service.NewFolderService(folderRepo)
	workspaceService := service.NewWorkspaceService(workspaceRepo)

	permissionChecker := repository.NewPermissionChecker(db)
	commentService := service.NewCommentService(repository.NewCommentRepository(db), permissionChecker, activityService)

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
	)
	authServ.WithAvatarRemover(fileService)

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database handle for readiness checks: %w", err)
	}

	hub := realtime.NewHub(realtime.DefaultMaxConnectionsPerUser, slog.Default())
	tickets := realtime.NewTicketStore(realtime.DefaultTicketTTL)

	return &Container{
		AuthService:         authServ,
		AuthHandler:         handler.NewAuthHandler(authServ, cfg.SecureCookies()),
		AccountEmailHandler: handler.NewAccountEmailHandler(accountEmail),
		ActivityHandler:     handler.NewActivityHandler(activityService),
		ChecklistHandler:    handler.NewChecklistHandler(service.NewChecklistService(repository.NewChecklistRepository(db), activityService)),
		FileHandler:         handler.NewFileHandler(fileService),
		CommentHandler:      handler.NewCommentHandler(commentService, taskService).WithPublisher(hub),
		TaskHandler:         handler.NewTaskHandler(taskService, cursorCodec).WithPublisher(hub).WithPermissionChecker(permissionChecker),
		UserHandler:         handler.NewUserHandler(userService),

		ContactHandler:    handler.NewContactHandler(contactService),
		SearchHandler:     handler.NewSearchHandler(service.NewSearchService(repository.NewSearchRepository(db))),
		SpaceHandler:      handler.NewSpaceHandler(spaceServ),
		StatusHandler:     handler.NewStatusHandler(statusServ),
		TagHandler:        handler.NewTagHandler(tagService),
		WorkspaceHandler:  handler.NewWorkspaceHandler(workspaceService),
		FolderHandler:     handler.NewFolderHandler(folderService),
		ListHandler:       handler.NewListHandler(spaceServ, folderService),
		PermissionChecker: permissionChecker,
		Ready:             sqlDB.PingContext,

		Realtime:        hub,
		RealtimeHandler: handler.NewRealtimeHandler(hub, tickets, permissionChecker, cfg.AllowedOrigins),
	}, nil
}
