package app

import (
	"os"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/handler"
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
}

func NewContainer(db *gorm.DB) *Container {

	userRepo := repository.NewUserRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	contactRepo := repository.NewContactRepository(db)
	spaceRepo := repository.NewSpaceRepository(db)
	statusRepo := repository.NewStatusRepository(db)
	tagRepo := repository.NewTagRepository(db)
	folderRepo := repository.NewFolderRepository(db)
	workspaceRepo := repository.NewWorkspaceRepository(db)

	jwtSecret := os.Getenv("JWT_SECRET")
	pepper := os.Getenv("APP_PEPPER")

	authServ := service.NewAuthService(userRepo, jwtSecret, pepper)
	spaceServ := service.NewSpaceService(spaceRepo)
	statusServ := service.NewStatusService(statusRepo, spaceRepo)
	tagService := service.NewTagService(tagRepo)
	taskService := service.NewTaskService(taskRepo, spaceRepo, tagService)
	userService := service.NewUserService(userRepo)
	contactService := service.NewContactService(contactRepo)
	folderService := service.NewFolderService(folderRepo)
	workspaceService := service.NewWorkspaceService(workspaceRepo)

	permissionChecker := service.NewPermissionChecker(db)

	return &Container{
		AuthService: authServ,
		AuthHandler: handler.NewAuthHandler(authServ),
		TaskHandler: handler.NewTaskHandler(taskService),
		UserHandler: handler.NewUserHandler(userService),

		ContactHandler:    handler.NewContactHandler(contactService),
		SpaceHandler:      handler.NewSpaceHandler(spaceServ),
		StatusHandler:     handler.NewStatusHandler(statusServ),
		TagHandler:        handler.NewTagHandler(tagService),
		WorkspaceHandler:  handler.NewWorkspaceHandler(workspaceService),
		FolderHandler:     handler.NewFolderHandler(folderService),
		ListHandler:       handler.NewListHandler(spaceServ, folderService),
		PermissionChecker: permissionChecker,
	}
}
