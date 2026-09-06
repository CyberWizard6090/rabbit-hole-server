package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	contextutil "rabbit-hole-server/internal/http/context"
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		id, err := strconv.ParseUint(c.Param(string(resource)), 10, 64)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid " + string(resource)})
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
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "permission check failed"})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: " + code})
			return
		}

		c.Next()
	}
}
