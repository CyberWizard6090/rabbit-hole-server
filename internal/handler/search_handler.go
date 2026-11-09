package handler

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type SearchHandler struct {
	service service.SearchService
}

func NewSearchHandler(service service.SearchService) *SearchHandler {
	return &SearchHandler{service: service}
}

var searchTypeNames = map[string]domain.SearchType{
	"tasks":           domain.SearchTypeTask,
	"comments":        domain.SearchTypeComment,
	"checklist_items": domain.SearchTypeChecklistItem,
}

func parseSearchTypes(raw string) ([]domain.SearchType, bool) {
	if strings.TrimSpace(raw) == "" {
		return domain.AllSearchTypes(), true
	}
	seen := map[domain.SearchType]bool{}
	var types []domain.SearchType
	for _, name := range strings.Split(raw, ",") {
		kind, ok := searchTypeNames[strings.TrimSpace(name)]
		if !ok {
			return nil, false
		}
		if !seen[kind] {
			seen[kind] = true
			types = append(types, kind)
		}
	}
	return types, true
}

func validateSearchQuery(raw string) (string, *httperrors.AppError) {
	query := strings.TrimSpace(raw)
	switch {
	case query == "":
		return "", httperrors.BadRequest("QUERY_REQUIRED", "query param 'q' is required", nil)
	case !utf8.ValidString(query) || strings.ContainsRune(query, 0):
		return "", httperrors.BadRequest("INVALID_QUERY", "query param 'q' must be valid text", nil)
	case utf8.RuneCountInString(query) < domain.MinSearchQueryRunes:
		return "", httperrors.BadRequest("QUERY_TOO_SHORT", "query param 'q' must contain at least 2 characters", nil)
	case utf8.RuneCountInString(query) > domain.MaxSearchQueryRunes:
		return "", httperrors.BadRequest("QUERY_TOO_LONG", "query param 'q' must contain at most 100 characters", nil)
	}
	return query, nil
}

func (h *SearchHandler) Search(c *gin.Context) {
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	query, apiErr := validateSearchQuery(c.Query("q"))
	if apiErr != nil {
		response.HandleError(c, apiErr)
		return
	}
	types, ok := parseSearchTypes(c.Query("types"))
	if !ok {
		response.HandleError(c, httperrors.BadRequest("INVALID_SEARCH_TYPES",
			"query param 'types' must be a comma-separated list of tasks, comments, checklist_items", nil))
		return
	}
	var spaceID *uint
	if raw := strings.TrimSpace(c.Query("space_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 63)
		if err != nil || id == 0 {
			response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space_id", err))
			return
		}
		value := uint(id)
		spaceID = &value
	}
	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}

	results, total, err := h.service.Search(c.Request.Context(), domain.SearchParams{
		WorkspaceID: workspaceID, Query: query, Types: types, SpaceID: spaceID, Limit: params.Limit, Offset: params.Offset,
	})
	if err != nil {
		response.HandleError(c, httperrors.Internal("SEARCH_FAILED", "internal server error", err))
		return
	}

	pages := (int(total) + params.Limit - 1) / params.Limit
	response.SuccessWithPagination(c, http.StatusOK, results, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: pages,
	})
}
