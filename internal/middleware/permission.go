package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	contextutil "rabbit-hole-server/internal/http/context"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type Resource string

const (
	ResourceWorkspace Resource = "workspace_id"
	ResourceSpace     Resource = "space_id"
	ResourceFolder    Resource = "folder_id"
	ResourceList      Resource = "list_id"
	ResourceTask      Resource = "id"
)

func RequirePermission(checker service.PermissionChecker, resource Resource, code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, err := contextutil.GetUserID(c)
		if err != nil {
			response.Unauthorized(c)
			return
		}

		id, err := strconv.ParseUint(c.Param(string(resource)), 10, 64)
		if err != nil {
			response.BadRequest(c, "invalid "+string(resource))
			return
		}

		var ok bool
		var checkErr error

		switch resource {
		case ResourceWorkspace:
			ok, checkErr = checker.HasWorkspacePermission(uid, uint(id), code)
		case ResourceSpace:
			ok, checkErr = checker.HasSpacePermission(uid, uint(id), code)
		case ResourceFolder:
			ok, checkErr = checker.HasFolderPermission(uid, uint(id), code)
		case ResourceList:
			ok, checkErr = checker.HasListPermission(uid, uint(id), code)
		case ResourceTask:
			ok, checkErr = checker.HasTaskPermission(uid, uint(id), code)
		}

		if checkErr != nil {
			response.Error(c, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !ok {
			response.Forbidden(c, "permission denied: "+code)
			return
		}

		c.Next()
	}
}
