package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/middleware"
)

const readyTimeout = 2 * time.Second

const (
	routeLists = "/lists"
	healthPath = "/healthz" // liveness: the process is up and serving
	readyPath  = "/readyz"  // readiness: dependencies answer too
)

func SetupRouter(deps *Container, cfg *config.Config) *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		panic(fmt.Errorf("configure trusted proxies: %w", err))
	}

	logger := slog.Default()
	r.Use(middleware.ExtractWSTicket(), middleware.RequestID(), middleware.AccessLog(logger, healthPath, readyPath), middleware.Recovery(logger))
	r.Use(middleware.SecurityHeaders(cfg.Environment == "production"))

	r.GET(healthPath, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET(readyPath, func(c *gin.Context) {
		if deps.Ready != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), readyTimeout)
			defer cancel()
			if err := deps.Ready(ctx); err != nil {
				slog.Warn("readiness check failed", "error", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.Use(middleware.BodyLimit(middleware.DefaultMaxBodyBytes))

	require := func(resource middleware.Resource) func(code string) gin.HandlerFunc {
		return func(code string) gin.HandlerFunc {
			return middleware.RequirePermission(deps.PermissionChecker, resource, code)
		}
	}
	var (
		inWorkspace = require(middleware.ResourceWorkspace)
		inSpace     = require(middleware.ResourceSpace)
		inFolder    = require(middleware.ResourceFolder)
		inList      = require(middleware.ResourceList)
		inTask      = require(middleware.ResourceTask)
	)
	ticketLimiter := middleware.RateLimiter(60, time.Minute)

	api := r.Group("/api")
	{
		v1 := api.Group("/v1")
		{
			// Each auth endpoint gets its own limiter instance, so a burst of
			// token refreshes cannot exhaust the login or registration budget.
			auth := v1.Group("/auth")
			{
				auth.POST("/register", middleware.RateLimiter(5, time.Minute), deps.AuthHandler.Register)
				auth.POST("/login", middleware.RateLimiter(10, time.Minute), deps.AuthHandler.Login)
				auth.POST("/refresh", middleware.RateLimiter(30, time.Minute), deps.AuthHandler.Refresh)
				// Logout authenticates with the refresh cookie, not the access
				// token, so it must work after the access token has expired.
				auth.POST("/logout", middleware.RateLimiter(30, time.Minute), deps.AuthHandler.Logout)
			}

			v1.GET("/lists/:list_id/events", deps.RealtimeHandler.ConnectList)
			v1.GET("/spaces/:space_id/events", deps.RealtimeHandler.ConnectSpace)

			protected := v1.Group("/")
			protected.Use(middleware.AuthMiddleware(deps.AuthService))
			{
				protected.POST("/auth/logout-all", middleware.RateLimiter(10, time.Minute), deps.AuthHandler.LogoutAll)

				profile := protected.Group("/profile")
				{
					profile.GET("", deps.UserHandler.GetCurrentUser)
					profile.PATCH("", deps.UserHandler.UpdateProfile)
					profile.POST("/password", middleware.RateLimiter(5, time.Minute), deps.AuthHandler.ChangePassword)
					profile.DELETE("", middleware.RateLimiter(5, time.Minute), deps.AuthHandler.DeleteAccount)
				}
				protected.GET("/users/search", deps.ContactHandler.Search)

				contacts := protected.Group("/contacts")
				{
					contacts.GET("", deps.ContactHandler.List)
					contacts.POST("", deps.ContactHandler.Add)
					contacts.DELETE("/:contact_id", deps.ContactHandler.Delete)
				}

				workspaces := protected.Group("/workspaces")
				{
					workspaces.POST("", deps.WorkspaceHandler.Create)
					workspaces.GET("", deps.WorkspaceHandler.List)

					workspaces.GET("/:workspace_id/members",
						inWorkspace(domain.PermWorkspaceRead),
						deps.WorkspaceHandler.ListMembers)
					workspaces.POST("/:workspace_id/members",
						inWorkspace(domain.PermMemberManage),
						deps.WorkspaceHandler.AddMember)
					workspaces.DELETE("/:workspace_id/members/me",
						inWorkspace(domain.PermWorkspaceRead),
						deps.WorkspaceHandler.Leave)
					workspaces.PATCH("/:workspace_id/members/:user_id",
						inWorkspace(domain.PermMemberManage),
						deps.WorkspaceHandler.UpdateMemberRole)
					workspaces.DELETE("/:workspace_id/members/:user_id",
						inWorkspace(domain.PermMemberManage),
						deps.WorkspaceHandler.RemoveMember)
					workspaces.GET("/:workspace_id",
						inWorkspace(domain.PermWorkspaceRead),
						deps.WorkspaceHandler.Get)
					workspaces.PATCH("/:workspace_id",
						inWorkspace(domain.PermWorkspaceUpdate),
						deps.WorkspaceHandler.Update)
					workspaces.DELETE("/:workspace_id",
						inWorkspace(domain.PermWorkspaceDelete),
						deps.WorkspaceHandler.Delete)

					workspaces.POST("/:workspace_id/spaces",
						inWorkspace(domain.PermSpaceCreate),
						deps.SpaceHandler.Create)
					workspaces.GET("/:workspace_id/spaces",
						inWorkspace(domain.PermSpaceRead),
						deps.SpaceHandler.List)
				}

				spaces := protected.Group("/spaces/:space_id")
				{
					spaces.GET("",
						inSpace(domain.PermSpaceRead),
						deps.SpaceHandler.Get)
					spaces.PATCH("",
						inSpace(domain.PermSpaceUpdate),
						deps.SpaceHandler.Update)
					spaces.DELETE("",
						inSpace(domain.PermSpaceDelete),
						deps.SpaceHandler.Delete)

					spaces.POST("/events/ticket",
						inSpace(domain.PermTaskRead),
						ticketLimiter, deps.RealtimeHandler.IssueSpaceTicket)

					spaces.GET("/dashboard",
						inSpace(domain.PermSpaceRead),
						deps.SpaceHandler.GetDashboard)

					spaces.POST("/tags",
						inSpace(domain.PermSpaceUpdate),
						deps.TagHandler.Create)
					spaces.GET("/tags",
						inSpace(domain.PermSpaceRead),
						deps.TagHandler.List)
					spaces.GET("/tags/:tag_id",
						inSpace(domain.PermSpaceRead),
						deps.TagHandler.Get)
					spaces.PUT("/tags/:tag_id",
						inSpace(domain.PermSpaceUpdate),
						deps.TagHandler.Update)
					spaces.DELETE("/tags/:tag_id",
						inSpace(domain.PermSpaceUpdate),
						deps.TagHandler.Delete)
					spaces.POST("/tags/merge",
						inSpace(domain.PermSpaceUpdate),
						deps.TagHandler.Merge)

					spaces.POST("/folders",
						inSpace(domain.PermSpaceUpdate),
						deps.FolderHandler.Create)
					spaces.GET("/folders",
						inSpace(domain.PermSpaceRead),
						deps.FolderHandler.List)

					spaces.POST(routeLists,
						inSpace(domain.PermSpaceUpdate),
						deps.ListHandler.CreateInSpace)
					spaces.GET(routeLists,
						inSpace(domain.PermSpaceRead),
						deps.ListHandler.ListInSpace)
				}

				folders := protected.Group("/folders/:folder_id")
				{
					folders.POST(routeLists,
						inFolder(domain.PermSpaceUpdate),
						deps.ListHandler.CreateInFolder)
					folders.GET(routeLists,
						inFolder(domain.PermSpaceRead),
						deps.ListHandler.ListInFolder)
					folders.PATCH("",
						inFolder(domain.PermSpaceUpdate),
						deps.FolderHandler.Update)
					folders.DELETE("",
						inFolder(domain.PermSpaceUpdate),
						deps.FolderHandler.Delete)
				}

				lists := protected.Group("/lists/:list_id")
				{
					lists.POST("/events/ticket",
						inList(domain.PermTaskRead),
						ticketLimiter, deps.RealtimeHandler.IssueListTicket)

					lists.POST("/statuses",
						inList(domain.PermSpaceUpdate),
						deps.StatusHandler.Create)
					lists.GET("/statuses",
						inList(domain.PermSpaceRead),
						deps.StatusHandler.List)
					lists.PUT("/statuses/:status_id",
						inList(domain.PermSpaceUpdate),
						deps.StatusHandler.Update)
					lists.DELETE("/statuses/:status_id",
						inList(domain.PermSpaceUpdate),
						deps.StatusHandler.Delete)

					lists.POST("/tasks",
						inList(domain.PermTaskCreate),
						deps.TaskHandler.Create)
					lists.GET("/tasks",
						inList(domain.PermTaskRead),
						deps.TaskHandler.List)
					lists.PATCH("",
						inList(domain.PermSpaceUpdate),
						deps.ListHandler.Update)
					lists.DELETE("",
						inList(domain.PermSpaceUpdate),
						deps.ListHandler.Delete)
				}

				tasks := protected.Group("/tasks/:task_id")
				{
					tasks.GET("",
						inTask(domain.PermTaskRead),
						deps.TaskHandler.Get)
					tasks.PATCH("",
						inTask(domain.PermTaskUpdate),
						deps.TaskHandler.Update)
					tasks.DELETE("",
						inTask(domain.PermTaskDelete),
						deps.TaskHandler.Delete)
					tasks.POST("/comments",
						inTask(domain.PermTaskUpdate),
						deps.CommentHandler.Create)
					tasks.GET("/comments",
						inTask(domain.PermTaskRead),
						deps.CommentHandler.List)
					tasks.PATCH("/comments/:comment_id",
						inTask(domain.PermTaskUpdate),
						deps.CommentHandler.Update)
					tasks.DELETE("/comments/:comment_id",
						inTask(domain.PermTaskRead),
						deps.CommentHandler.Delete)
					tasks.POST("/move",
						inTask(domain.PermTaskUpdate),
						deps.TaskHandler.Move)
				}
			}
		}
	}
	return r
}
