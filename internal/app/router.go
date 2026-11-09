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
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/middleware"
)

const readyTimeout = 2 * time.Second

const (
	routeLists = "/lists"
	healthPath = "/healthz" // liveness: the process is up and serving
	readyPath  = "/readyz"  // readiness: dependencies answer too
)

const (
	multipartOverhead   int64 = 1 << 20
	publicFormBodyLimit int64 = 128 << 10
)

var multipartRoutes = map[string]bool{
	"POST /api/v1/tasks/:task_id/attachments":      true,
	"POST /api/v1/workspaces/:workspace_id/images": true,
	"POST /api/v1/profile/avatar":                  true,
	"POST /api/v1/workspaces/:workspace_id/avatar": true,
	"POST /api/v1/public/forms/:slug/uploads":      true,
}

func BodyLimitOverrides(cfg *config.Config) map[string]int64 {
	return map[string]int64{
		"POST /api/v1/tasks/:task_id/attachments":      cfg.Storage.MaxAttachmentBytes + multipartOverhead,
		"POST /api/v1/workspaces/:workspace_id/images": cfg.Storage.MaxImageBytes + multipartOverhead,
		"POST /api/v1/profile/avatar":                  cfg.Storage.MaxAvatarBytes + multipartOverhead,
		"POST /api/v1/workspaces/:workspace_id/avatar": cfg.Storage.MaxAvatarBytes + multipartOverhead,
		"POST /api/v1/public/forms/:slug/submissions":  publicFormBodyLimit,
		"POST /api/v1/public/forms/:slug/uploads":      cfg.Forms.UploadMaxBytes + multipartOverhead,
	}
}

func SetupRouter(deps *Container, cfg *config.Config) *gin.Engine {
	r := gin.New()
	response.RegisterValidatorFieldNames()
	if err := r.SetTrustedProxies(cfg.Server.Proxies()); err != nil {
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
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "Idempotency-Key"},
		ExposeHeaders:    []string{"Content-Length", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.MaxMultipartMemory = 8 << 20
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, http.StatusNotFound, "ROUTE_NOT_FOUND", nil)
	})
	r.NoMethod(func(c *gin.Context) {
		response.Fail(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", nil)
	})
	r.Use(middleware.BodyLimitByRoute(middleware.DefaultMaxBodyBytes, BodyLimitOverrides(cfg)))
	r.Use(middleware.PathIDs(), middleware.JSONBody(multipartRoutes))

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
	limit := func(bucket string, max int, period time.Duration) gin.HandlerFunc {
		return middleware.RateLimiter(deps.RateLimiter, bucket, max, period)
	}
	ticketLimiter := limit("realtime-tickets", 60, time.Minute)
	uploadLimiter := limit("uploads", 60, time.Minute)
	channelMailLimiter := limit("notification-channel-mail", 20, time.Minute)
	contactRequestLimiter := limit("contact-requests", 20, time.Minute)
	userSearchLimiter := limit("user-search", 30, time.Minute)
	rehostLimiter := limit("image-rehost", 30, time.Minute)
	formUploadLimiter := limit("form-uploads", 30, time.Minute)

	api := r.Group("/api")
	{
		v1 := api.Group("/v1")
		{
			// Each auth endpoint gets its own limiter instance, so a burst of
			// token refreshes cannot exhaust the login or registration budget.
			auth := v1.Group("/auth")
			{
				auth.POST("/register", limit("auth.register", 5, time.Minute), deps.AuthHandler.Register)
				auth.POST("/login", limit("auth.login", 10, time.Minute), deps.AuthHandler.Login)
				auth.POST("/refresh", limit("auth.refresh", 30, time.Minute), deps.AuthHandler.Refresh)
				auth.POST("/verify-email", limit("auth.verify-email", 20, time.Minute), deps.AccountEmailHandler.VerifyEmail)
				auth.POST("/resend-verification", limit("auth.resend-verification", 5, time.Minute), deps.AccountEmailHandler.ResendVerification)
				auth.POST("/forgot-password", limit("auth.forgot-password", 5, time.Minute), deps.AccountEmailHandler.ForgotPassword)
				auth.POST("/reset-password", limit("auth.reset-password", 10, time.Minute), deps.AccountEmailHandler.ResetPassword)
				auth.POST("/confirm-email-change", limit("auth.confirm-email-change", 10, time.Minute), deps.EmailChangeHandler.Confirm)
				// Logout authenticates with the refresh cookie, not the access
				// token, so it must work after the access token has expired.
				auth.POST("/logout", limit("auth.logout", 30, time.Minute), deps.AuthHandler.Logout)
			}

			v1.POST("/notifications/channels/confirm", limit("notifications.channels.confirm", 20, time.Minute), deps.NotificationHandler.ConfirmChannel)

			v1.GET("/images/:public_id", deps.FileHandler.ServeImage)
			v1.GET("/public/forms/:slug", deps.FormHandler.GetPublic)
			v1.POST("/public/forms/:slug/submissions",
				limit("public-form-submissions", 5, time.Minute),
				middleware.OptionalAuthMiddleware(deps.AuthService),
				deps.FormHandler.Submit)
			v1.POST("/public/forms/:slug/uploads", formUploadLimiter, deps.FormUploadHandler.Upload)

			v1.GET("/notifications/events", deps.RealtimeHandler.ConnectNotifications)
			v1.GET("/lists/:list_id/events", deps.RealtimeHandler.ConnectList)
			v1.GET("/spaces/:space_id/events", deps.RealtimeHandler.ConnectSpace)

			protected := v1.Group("/")
			protected.Use(middleware.AuthMiddleware(deps.AuthService))
			{
				protected.POST("/auth/logout-all", limit("auth.logout-all", 10, time.Minute), deps.AuthHandler.LogoutAll)

				profile := protected.Group("/profile")
				{
					profile.GET("", deps.UserHandler.GetCurrentUser)
					profile.PATCH("", deps.UserHandler.UpdateProfile)
					profile.POST("/password", limit("profile.password", 5, time.Minute), deps.AuthHandler.ChangePassword)
					profile.POST("/email", limit("profile.email", 5, time.Minute), deps.EmailChangeHandler.Request)
					profile.DELETE("/email-change", limit("profile.email-change", 10, time.Minute), deps.EmailChangeHandler.Cancel)
					profile.DELETE("", limit("profile.delete", 5, time.Minute), deps.AuthHandler.DeleteAccount)
				}
				protected.GET("/time/running", deps.TimeHandler.Running)
				protected.POST("/tasks/bulk", limit("tasks.bulk", 30, time.Minute), deps.TaskHandler.Bulk)
				profile.POST("/avatar", uploadLimiter, deps.FileHandler.SetAvatar)
				profile.DELETE("/avatar", deps.FileHandler.RemoveAvatar)
				protected.GET("/users/search", userSearchLimiter, deps.ContactHandler.Search)

				contacts := protected.Group("/contacts")
				{
					contacts.GET("", deps.ContactHandler.List)
					contacts.POST("", contactRequestLimiter, deps.ContactHandler.Add)
					contacts.GET("/requests/incoming", deps.ContactHandler.ListIncoming)
					contacts.GET("/requests/outgoing", deps.ContactHandler.ListOutgoing)
					contacts.POST("/requests/:user_id/accept", deps.ContactHandler.Accept)
					contacts.POST("/requests/:user_id/decline", deps.ContactHandler.Decline)
					contacts.DELETE("/requests/:user_id", deps.ContactHandler.Cancel)
					contacts.DELETE("/:contact_id", deps.ContactHandler.Delete)
				}

				notifications := protected.Group("/notifications")
				{
					notifications.GET("", deps.NotificationHandler.List)
					notifications.GET("/unread-count", deps.NotificationHandler.UnreadCount)
					notifications.POST("/events/ticket", ticketLimiter, deps.RealtimeHandler.IssueNotificationTicket)
					notifications.POST("/read", deps.NotificationHandler.MarkRead)
					notifications.GET("/preferences", deps.NotificationHandler.Preferences)
					notifications.PUT("/preferences", deps.NotificationHandler.SavePreferences)
					notifications.GET("/settings", deps.NotificationHandler.GetSettings)
					notifications.PUT("/settings", deps.NotificationHandler.SaveSettings)
					notifications.GET("/channels", deps.NotificationHandler.ListChannels)
					notifications.POST("/channels", channelMailLimiter, deps.NotificationHandler.AddChannel)
					notifications.POST("/channels/:channel_id/resend", channelMailLimiter, deps.NotificationHandler.ResendChannel)
					notifications.POST("/channels/:channel_id/rotate-secret", channelMailLimiter, deps.NotificationHandler.RotateChannelSecret)
					notifications.DELETE("/channels/:channel_id", deps.NotificationHandler.DeleteChannel)
					notifications.DELETE("/:notification_id", deps.NotificationHandler.Delete)
				}

				protected.GET("/form-templates", deps.FormHandler.ListTemplates)
				protected.GET("/list-templates", deps.ListHandler.Templates)
				protected.GET("/task-templates/:template_id", deps.TaskTemplateHandler.Get)
				protected.PATCH("/task-templates/:template_id", deps.TaskTemplateHandler.Update)
				protected.DELETE("/task-templates/:template_id", deps.TaskTemplateHandler.Archive)
				protected.POST("/task-templates/:template_id/versions", deps.TaskTemplateHandler.CreateVersion)
				protected.GET("/task-templates/:template_id/versions", deps.TaskTemplateHandler.ListVersions)
				protected.POST("/task-templates/:template_id/apply", deps.TaskTemplateHandler.Apply)
				protected.GET("/submissions/:submission_id/tasks", deps.FormTaskHandler.ListSubmissionTasks)
				protected.GET("/submissions/:submission_id/task-status", deps.FormTaskHandler.TaskStatus)
				protected.POST("/submissions/:submission_id/retry-task-creation", deps.FormTaskHandler.Retry)

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
					workspaces.GET("/:workspace_id/time-report",
						inWorkspace(domain.PermTaskRead),
						deps.TimeHandler.Report)
					workspaces.GET("/:workspace_id/views",
						inWorkspace(domain.PermTaskRead),
						deps.SavedViewHandler.List)
					workspaces.POST("/:workspace_id/views",
						inWorkspace(domain.PermTaskRead),
						deps.SavedViewHandler.Create)
					workspaces.PATCH("/:workspace_id/views/:view_id",
						inWorkspace(domain.PermTaskRead),
						deps.SavedViewHandler.Update)
					workspaces.DELETE("/:workspace_id/views/:view_id",
						inWorkspace(domain.PermTaskRead),
						deps.SavedViewHandler.Delete)
					workspaces.GET("/:workspace_id/audit-log",
						inWorkspace(domain.PermMemberManage),
						deps.WorkspaceHandler.AuditLog)
					workspaces.POST("/:workspace_id/avatar",
						inWorkspace(domain.PermWorkspaceUpdate),
						uploadLimiter,
						deps.FileHandler.SetWorkspaceAvatar)
					workspaces.DELETE("/:workspace_id/avatar",
						inWorkspace(domain.PermWorkspaceUpdate),
						deps.FileHandler.RemoveWorkspaceAvatar)
					workspaces.DELETE("/:workspace_id",
						inWorkspace(domain.PermWorkspaceDelete),
						deps.WorkspaceHandler.Delete)

					workspaces.GET("/:workspace_id/search",
						inWorkspace(domain.PermWorkspaceRead),
						deps.SearchHandler.Search)

					workspaces.POST("/:workspace_id/forms",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.Create)
					workspaces.GET("/:workspace_id/forms",
						inWorkspace(domain.PermFormRead),
						deps.FormHandler.List)
					workspaces.GET("/:workspace_id/forms/:form_id",
						inWorkspace(domain.PermFormRead),
						deps.FormHandler.Get)
					workspaces.PATCH("/:workspace_id/forms/:form_id",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.Update)
					workspaces.DELETE("/:workspace_id/forms/:form_id",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.Delete)
					workspaces.POST("/:workspace_id/forms/:form_id/publish",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.Publish)
					workspaces.POST("/:workspace_id/forms/:form_id/archive",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.Archive)
					workspaces.POST("/:workspace_id/forms/:form_id/questions",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.CreateQuestion)
					workspaces.PATCH("/:workspace_id/forms/:form_id/questions/:question_id",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.UpdateQuestion)
					workspaces.DELETE("/:workspace_id/forms/:form_id/questions/:question_id",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.DeleteQuestion)
					workspaces.PUT("/:workspace_id/forms/:form_id/questions/reorder",
						inWorkspace(domain.PermFormManage),
						deps.FormHandler.ReorderQuestions)
					workspaces.GET("/:workspace_id/forms/:form_id/submissions",
						inWorkspace(domain.PermFormRead),
						deps.FormHandler.ListSubmissions)
					workspaces.GET("/:workspace_id/forms/:form_id/task-config",
						inWorkspace(domain.PermFormRead),
						deps.FormTaskHandler.GetConfig)
					workspaces.PUT("/:workspace_id/forms/:form_id/task-config",
						inWorkspace(domain.PermFormManage),
						deps.FormTaskHandler.SetConfig)
					workspaces.GET("/:workspace_id/submissions/:submission_id",
						inWorkspace(domain.PermFormRead),
						deps.FormHandler.GetSubmission)
					workspaces.GET("/:workspace_id/submissions/:submission_id/files/:file_id",
						inWorkspace(domain.PermFormRead),
						deps.FormUploadHandler.DownloadSubmissionFile)

					workspaces.POST("/:workspace_id/custom-fields",
						inWorkspace(domain.PermFieldManage),
						deps.CustomFieldHandler.Create)
					workspaces.GET("/:workspace_id/custom-fields",
						inWorkspace(domain.PermWorkspaceRead),
						deps.CustomFieldHandler.List)
					workspaces.GET("/:workspace_id/custom-fields/:field_id",
						inWorkspace(domain.PermWorkspaceRead),
						deps.CustomFieldHandler.Get)
					workspaces.PATCH("/:workspace_id/custom-fields/:field_id",
						inWorkspace(domain.PermFieldManage),
						deps.CustomFieldHandler.Update)
					workspaces.DELETE("/:workspace_id/custom-fields/:field_id",
						inWorkspace(domain.PermFieldManage),
						deps.CustomFieldHandler.Delete)
					workspaces.POST("/:workspace_id/task-templates",
						inWorkspace(domain.PermTemplateManage),
						deps.TaskTemplateHandler.Create)
					workspaces.GET("/:workspace_id/task-templates",
						inWorkspace(domain.PermTemplateRead),
						deps.TaskTemplateHandler.List)

					workspaces.POST("/:workspace_id/images",
						inWorkspace(domain.PermTaskUpdate),
						uploadLimiter, deps.FileHandler.UploadImage)

					workspaces.POST("/:workspace_id/images/rehost",
						inWorkspace(domain.PermTaskUpdate),
						rehostLimiter, deps.FileHandler.RehostImage)

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
					tasks.GET("/custom-fields",
						inTask(domain.PermTaskRead),
						deps.CustomFieldHandler.GetTaskValues)
					tasks.PUT("/custom-fields/:field_id",
						inTask(domain.PermTaskUpdate),
						deps.CustomFieldHandler.SetTaskValue)
					tasks.DELETE("/custom-fields/:field_id",
						inTask(domain.PermTaskUpdate),
						deps.CustomFieldHandler.DeleteTaskValue)
					tasks.GET("",
						inTask(domain.PermTaskRead),
						deps.TaskHandler.Get)
					tasks.PATCH("",
						inTask(domain.PermTaskUpdate),
						deps.TaskHandler.Update)
					tasks.DELETE("",
						inTask(domain.PermTaskDelete),
						deps.TaskHandler.Delete)
					tasks.GET("/time",
						inTask(domain.PermTaskRead),
						deps.TimeHandler.List)
					tasks.POST("/time",
						inTask(domain.PermTaskUpdate),
						deps.TimeHandler.Add)
					tasks.POST("/time/start",
						inTask(domain.PermTaskUpdate),
						deps.TimeHandler.Start)
					tasks.POST("/time/stop",
						inTask(domain.PermTaskUpdate),
						deps.TimeHandler.Stop)
					tasks.PATCH("/time/:entry_id",
						inTask(domain.PermTaskUpdate),
						deps.TimeHandler.Update)
					tasks.DELETE("/time/:entry_id",
						inTask(domain.PermTaskUpdate),
						deps.TimeHandler.Delete)
					tasks.GET("/watchers",
						inTask(domain.PermTaskRead),
						deps.NotificationHandler.Watchers)
					tasks.PUT("/watch",
						inTask(domain.PermTaskRead),
						deps.NotificationHandler.Watch)
					tasks.DELETE("/watch",
						inTask(domain.PermTaskRead),
						deps.NotificationHandler.Unwatch)
					tasks.GET("/activity",
						inTask(domain.PermTaskRead),
						deps.ActivityHandler.List)
					tasks.POST("/attachments",
						inTask(domain.PermTaskUpdate),
						uploadLimiter, deps.FileHandler.UploadAttachment)
					tasks.GET("/attachments",
						inTask(domain.PermTaskRead),
						deps.FileHandler.ListAttachments)
					tasks.GET("/attachments/:attachment_id/download",
						inTask(domain.PermTaskRead),
						deps.FileHandler.DownloadAttachment)
					tasks.DELETE("/attachments/:attachment_id",
						inTask(domain.PermTaskRead),
						deps.FileHandler.DeleteAttachment)
					tasks.GET("/checklists",
						inTask(domain.PermTaskRead),
						deps.ChecklistHandler.List)
					tasks.POST("/checklists",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.Create)
					tasks.PUT("/checklists/order",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.Reorder)
					tasks.PATCH("/checklists/:checklist_id",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.Rename)
					tasks.DELETE("/checklists/:checklist_id",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.Delete)
					tasks.POST("/checklists/:checklist_id/items",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.CreateItem)
					tasks.PUT("/checklists/:checklist_id/items/order",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.ReorderItems)
					tasks.PATCH("/checklists/:checklist_id/items/:item_id",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.UpdateItem)
					tasks.DELETE("/checklists/:checklist_id/items/:item_id",
						inTask(domain.PermTaskUpdate),
						deps.ChecklistHandler.DeleteItem)
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
