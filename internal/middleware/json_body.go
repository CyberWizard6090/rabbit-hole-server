package middleware

import (
	"bytes"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"

	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/jsonscan"
	"rabbit-hole-server/internal/http/response"
)

const jsonMediaType = "application/json"

func JSONBody(skipRoutes map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !carriesJSON(c, skipRoutes) {
			c.Next()
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			response.ValidationError(c, err)
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		if len(body) == 0 {
			c.Next()
			return
		}
		if !isJSONContentType(c.GetHeader("Content-Type")) {
			response.Fail(c, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", map[string]any{"expected": jsonMediaType})
			c.Abort()
			return
		}
		if issues := jsonscan.Scan(body); len(issues) > 0 {
			response.FailWithDetails(c, http.StatusBadRequest, "VALIDATION_FAILED", nil, scanDetails(issues))
			c.Abort()
			return
		}
		c.Next()
	}
}

func carriesJSON(c *gin.Context, skipRoutes map[string]bool) bool {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	route := c.FullPath()
	if route == "" || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return false
	}
	return !skipRoutes[c.Request.Method+" "+route]
}

func isJSONContentType(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && mediaType == jsonMediaType
}

func scanDetails(issues []jsonscan.Issue) []httperrors.Detail {
	details := make([]httperrors.Detail, 0, len(issues))
	for _, issue := range issues {
		details = append(details, httperrors.Detail{Field: issue.Field, Code: issue.Code, Params: issue.Params})
	}
	return details
}
