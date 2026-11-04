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
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/repository"
	"rabbit-hole-server/internal/service"
)

type Container struct {
	AuthService *service.AuthService
	AuthHandler *handler.AuthHandler
	TaskHandler *handler.TaskHandler
	UserHandler *handler.UserHandler

	WorkspaceHandler *handler.WorkspaceHandler
	FolderHandler    *handler.FolderHandler
	ListHandler      *handler.ListHandler

	ContactHandler *handler.ContactHandler
	SpaceHandler   *handler.SpaceHandler
	StatusHandler  *handler.StatusHandler
	TagHandler     *handler.TagHandler

	PermissionChecker service.PermissionChecker

	Ready           func(ctx context.Context) error
	Realtime        *realtime.Hub
	RealtimeHandler *handler.RealtimeHandler
}

func NewContainer(db *gorm.DB, cfg *config.Config) (*Container, error) {
	userRepo := repository.NewUserRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	contactRepo := repository.NewContactRepository(db)
	spaceRepo := repository.NewSpaceRepository(db)
	statusRepo := repository.NewStatusRepository(db)
	tagRepo := repository.NewTagRepository(db)
	folderRepo := repository.NewFolderRepository(db)
	workspaceRepo := repository.NewWorkspaceRepository(db)

	authServ := service.NewAuthService(userRepo, cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL)
	spaceServ := service.NewSpaceService(spaceRepo)
	statusServ := service.NewStatusService(statusRepo, spaceRepo)
	tagService := service.NewTagService(tagRepo)
	keyDerivation := hmac.New(sha256.New, []byte(cfg.JWT.Secret))
	_, _ = keyDerivation.Write([]byte("rabbit-hole-server/task-cursor-signing/v1"))
	cursorCodec, err := pagination.NewCodec(keyDerivation.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create task cursor codec: %w", err)
	}
	taskService := service.NewTaskService(taskRepo, spaceRepo, tagService, cursorCodec)
	userService := service.NewUserService(userRepo)
	contactService := service.NewContactService(contactRepo)
	folderService := service.NewFolderService(folderRepo)
	workspaceService := service.NewWorkspaceService(workspaceRepo)

	permissionChecker := repository.NewPermissionChecker(db)

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database handle for readiness checks: %w", err)
	}

	hub := realtime.NewHub(realtime.DefaultMaxConnectionsPerUser, slog.Default())
	tickets := realtime.NewTicketStore(realtime.DefaultTicketTTL)

	return &Container{
		AuthService: authServ,
		AuthHandler: handler.NewAuthHandler(authServ, cfg.SecureCookies()),
		TaskHandler: handler.NewTaskHandler(taskService, cursorCodec).WithPublisher(hub).WithPermissionChecker(permissionChecker),
		UserHandler: handler.NewUserHandler(userService),

		ContactHandler:    handler.NewContactHandler(contactService),
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
