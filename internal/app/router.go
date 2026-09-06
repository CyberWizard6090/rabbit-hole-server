package app

import (
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/middleware"
)

const routeLists = "/lists"

func SetupRouter(deps *Container) *gin.Engine {
	r := gin.New()

	r.Use(gin.Logger(), gin.Recovery())

	r.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	})

	allowedOrigin := os.Getenv("ALLOWED_ORIGIN")
	if allowedOrigin == "" {
		allowedOrigin = "http://127.0.0.1:3000"
	}

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{allowedOrigin},
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
					profile.GET("/", deps.UserHandler.GetMe)
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
					workspaces.GET("/", deps.WorkspaceHandler.GetAllForUser)

					workspaces.POST("/:workspace_id/spaces",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceWorkspace, "space.create"),
						deps.SpaceHandler.CreateSpace)
					workspaces.GET("/:workspace_id/spaces",
						middleware.RequirePermission(deps.PermissionChecker, middleware.ResourceWorkspace, "space.read"),
						deps.SpaceHandler.GetAll)
				}

				spaces := protected.Group("/spaces/:space_id")
				{
					spaces.GET("/dashboard", deps.SpaceHandler.GetDashboard)

					spaces.POST("/tags", deps.TagHandler.Create)
					spaces.GET("/tags", deps.TagHandler.GetAll)
					spaces.PUT("/tags/:tag_id", deps.TagHandler.Update)
					spaces.DELETE("/tags/:tag_id", deps.TagHandler.Delete)
					spaces.POST("/tags/merge", deps.TagHandler.Merge)

					spaces.POST("/folders", deps.FolderHandler.Create)
					spaces.GET("/folders", deps.FolderHandler.GetAll)

					spaces.POST(routeLists, deps.ListHandler.CreateInSpace)
					spaces.GET(routeLists, deps.ListHandler.GetAllInSpace)
				}

				folders := protected.Group("/folders/:folder_id")
				{
					folders.POST(routeLists, deps.ListHandler.CreateInFolder)
					folders.GET(routeLists, deps.ListHandler.GetAllInFolder)
					folders.PATCH("/", deps.FolderHandler.Update)
					folders.DELETE("/", deps.FolderHandler.Delete)
				}

				lists := protected.Group("/lists/:list_id")
				{
					lists.POST("/statuses", deps.StatusHandler.Create)
					lists.GET("/statuses", deps.StatusHandler.GetAll)
					lists.PUT("/statuses/:status_id", deps.StatusHandler.Update)
					lists.DELETE("/statuses/:status_id", deps.StatusHandler.Delete)

					lists.POST("/tasks", deps.TaskHandler.Create)
					lists.GET("/tasks", deps.TaskHandler.GetAll)
					lists.PATCH("/", deps.ListHandler.Update)
					lists.DELETE("/", deps.ListHandler.Delete)
				}

				tasks := protected.Group("/tasks/:id")
				{
					tasks.GET("/", deps.TaskHandler.GetByID)
					tasks.PATCH("/", deps.TaskHandler.Update)
					tasks.DELETE("/", deps.TaskHandler.Delete)
				}
			}
		}
	}
	return r
}
