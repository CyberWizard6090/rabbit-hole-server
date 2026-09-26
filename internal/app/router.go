package app

import (
	"fmt"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/middleware"
)

const routeLists = "/lists"

func SetupRouter(deps *Container, cfg *config.Config) *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		panic(fmt.Errorf("configure trusted proxies: %w", err))
	}

	r.Use(gin.Logger(), gin.Recovery())

	r.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	})

	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	rateLimiter := middleware.RateLimiter(10, time.Minute)

	api := r.Group("/api")
	{
		v1 := api.Group("/v1")
		{
			auth := v1.Group("/auth")
			auth.Use(rateLimiter)
			{
				auth.POST("/register", deps.AuthHandler.Register)
				auth.POST("/login", deps.AuthHandler.Login)
				auth.POST("/refresh", deps.AuthHandler.Refresh)
			}

			protected := v1.Group("/")
			protected.Use(middleware.AuthMiddleware(deps.AuthService))
			{
				protected.POST("/auth/logout", deps.AuthHandler.Logout)

				profile := protected.Group("/profile")
				{
					profile.GET("/", deps.UserHandler.GetCurrentUser)
					profile.PATCH("/", deps.UserHandler.UpdateProfile)
				}
				protected.GET("/users/search", deps.ContactHandler.Search)

				contacts := protected.Group("/contacts")
				{
					contacts.GET("/", deps.ContactHandler.List)
					contacts.POST("/", deps.ContactHandler.Add)
					contacts.DELETE("/:id", deps.ContactHandler.Delete)
				}

				workspaces := protected.Group("/workspaces")
				{
					workspaces.POST("/", deps.WorkspaceHandler.Create)
					workspaces.GET("/", deps.WorkspaceHandler.List)

					workspaces.POST("/:workspace_id/spaces",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceWorkspace, "space.create"),
						deps.SpaceHandler.Create)
					workspaces.GET("/:workspace_id/spaces",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceWorkspace, "space.read"),
						deps.SpaceHandler.List)
				}

				spaces := protected.Group("/spaces/:space_id")
				{
					spaces.GET("/dashboard",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.read"),
						deps.SpaceHandler.GetDashboard)

					spaces.POST("/tags",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.update"),
						deps.TagHandler.Create)
					spaces.GET("/tags",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.read"),
						deps.TagHandler.List)
					spaces.PUT("/tags/:tag_id",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.update"),
						deps.TagHandler.Update)
					spaces.DELETE("/tags/:tag_id",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.update"),
						deps.TagHandler.Delete)
					spaces.POST("/tags/merge",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.update"),
						deps.TagHandler.Merge)

					spaces.POST("/folders",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.update"),
						deps.FolderHandler.Create)
					spaces.GET("/folders",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.read"),
						deps.FolderHandler.List)

					spaces.POST(routeLists,
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.update"),
						deps.ListHandler.CreateInSpace)
					spaces.GET(routeLists,
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceSpace, "space.read"),
						deps.ListHandler.ListInSpace)
				}

				folders := protected.Group("/folders/:folder_id")
				{
					folders.POST(routeLists,
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceFolder, "space.update"),
						deps.ListHandler.CreateInFolder)
					folders.GET(routeLists,
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceFolder, "space.read"),
						deps.ListHandler.ListInFolder)
					folders.PATCH("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceFolder, "space.update"),
						deps.FolderHandler.Update)
					folders.DELETE("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceFolder, "space.update"),
						deps.FolderHandler.Delete)
				}

				lists := protected.Group("/lists/:list_id")
				{
					lists.POST("/statuses",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "space.update"),
						deps.StatusHandler.Create)
					lists.GET("/statuses",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "space.read"),
						deps.StatusHandler.List)
					lists.PUT("/statuses/:status_id",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "space.update"),
						deps.StatusHandler.Update)
					lists.DELETE("/statuses/:status_id",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "space.update"),
						deps.StatusHandler.Delete)

					lists.POST("/tasks",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "task.create"),
						deps.TaskHandler.Create)
					lists.GET("/tasks",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "task.read"),
						deps.TaskHandler.List)
					lists.PATCH("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "space.update"),
						deps.ListHandler.Update)
					lists.DELETE("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceList, "space.update"),
						deps.ListHandler.Delete)
				}

				tasks := protected.Group("/tasks/:id")
				{
					tasks.GET("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceTask, "task.read"),
						deps.TaskHandler.Get)
					tasks.PATCH("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceTask, "task.update"),
						deps.TaskHandler.Update)
					tasks.DELETE("/",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceTask, "task.delete"),
						deps.TaskHandler.Delete)
				}
			}
		}
	}
	return r
}
