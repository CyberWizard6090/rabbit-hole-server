package integration

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	publicRoutes = map[string]string{
		"POST /api/v1/auth/register":                  "anonymous",
		"POST /api/v1/auth/login":                     "anonymous",
		"POST /api/v1/auth/refresh":                   "authenticated by refresh cookie",
		"POST /api/v1/auth/logout":                    "authenticated by refresh cookie",
		"POST /api/v1/auth/verify-email":              "authenticated by a single-use emailed token",
		"POST /api/v1/auth/resend-verification":       "anonymous; answers the same for every address",
		"POST /api/v1/auth/forgot-password":           "anonymous; answers the same for every address",
		"POST /api/v1/auth/reset-password":            "authenticated by a single-use emailed token",
		"POST /api/v1/auth/confirm-email-change":      "authenticated by a single-use token emailed to the new address",
		"POST /api/v1/notifications/channels/confirm": "authenticated by a single-use emailed token; answers the same for every address",
		"GET /api/v1/notifications/events":            "authenticated by a single-use ticket issued to the signed-in user for their own feed",
		"GET /api/v1/lists/:list_id/events":           "authenticated by a single-use ticket issued after a permission check",
		"GET /api/v1/spaces/:space_id/events":         "authenticated by a single-use ticket issued after a permission check",
		"GET /api/v1/images/:public_id":               "editor images and avatars are public by design; the id is a random 128-bit value",
		"GET /api/v1/public/forms/:slug":              "published form schema is public",
		"POST /api/v1/public/forms/:slug/submissions": "published form submission is public and rate limited",
		"POST /api/v1/public/forms/:slug/uploads":     "file answers of a published form are uploaded anonymously; rate limited, size and type checked",
	}
	selfScopedRoutes = map[string]string{
		"GET /api/v1/profile":                             "caller's own profile",
		"PATCH /api/v1/profile":                           "caller's own profile",
		"DELETE /api/v1/profile":                          "deletes the caller's own account",
		"POST /api/v1/profile/password":                   "changes the caller's own password",
		"POST /api/v1/profile/email":                      "starts a change of the caller's own email, password required",
		"DELETE /api/v1/profile/email-change":             "cancels the caller's own pending email change",
		"POST /api/v1/profile/avatar":                     "replaces the caller's own avatar",
		"DELETE /api/v1/profile/avatar":                   "removes the caller's own avatar",
		"POST /api/v1/auth/logout-all":                    "revokes the caller's own sessions",
		"GET /api/v1/users/search":                        "public projection only",
		"GET /api/v1/form-templates":                      "static catalog of form presets, no workspace data",
		"GET /api/v1/list-templates":                      "static catalog of list templates, no workspace data",
		"GET /api/v1/time/running":                        "the caller's own running timer",
		"POST /api/v1/tasks/bulk":                         "permission is checked for every task in the body, covered by TestBulkTasks_PermissionsAreCheckedForEveryTask",
		"GET /api/v1/contacts":                            "caller's own contacts",
		"POST /api/v1/contacts":                           "creates a pending request from the caller; the answer is identical for every target",
		"GET /api/v1/contacts/requests/incoming":          "caller's own incoming contact requests",
		"GET /api/v1/contacts/requests/outgoing":          "caller's own outgoing contact requests",
		"POST /api/v1/contacts/requests/:user_id/accept":  "scoped to the caller in SQL; others get 404",
		"POST /api/v1/contacts/requests/:user_id/decline": "scoped to the caller in SQL; others get 404",
		"DELETE /api/v1/contacts/requests/:user_id":       "scoped to the caller in SQL; others get 404",
		"DELETE /api/v1/contacts/:contact_id":             "scoped to caller in SQL",
		"POST /api/v1/workspaces":                         "creates a workspace owned by the caller",
		"GET /api/v1/workspaces":                          "lists the caller's workspaces",

		"GET /api/v1/notifications":                              "caller's own inbox; entries about unreadable tasks are hidden",
		"GET /api/v1/notifications/unread-count":                 "caller's own unread counts",
		"POST /api/v1/notifications/events/ticket":               "issues a ticket for the caller's own notification feed",
		"POST /api/v1/notifications/read":                        "scoped to the caller in SQL",
		"DELETE /api/v1/notifications/:notification_id":          "scoped to the caller in SQL; others get 404",
		"GET /api/v1/notifications/preferences":                  "caller's own preferences",
		"PUT /api/v1/notifications/preferences":                  "caller's own preferences",
		"GET /api/v1/notifications/settings":                     "caller's own notification settings",
		"PUT /api/v1/notifications/settings":                     "caller's own notification settings",
		"GET /api/v1/notifications/channels":                     "caller's own delivery destinations",
		"POST /api/v1/notifications/channels":                    "adds a destination to the caller's own account",
		"POST /api/v1/notifications/channels/:channel_id/resend": "scoped to the caller in SQL; others get 404",
		"DELETE /api/v1/notifications/channels/:channel_id":      "scoped to the caller in SQL; others get 404",

		"POST /api/v1/notifications/channels/:channel_id/rotate-secret": "scoped to the caller in SQL; others get 404",
	}
)

type routeFixture struct {
	params map[string]string
}

func buildRouteFixture(t *testing.T, tc *TestContext, token string, userID uint) routeFixture {
	t.Helper()
	create := func(path string, body map[string]any) uint {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPost, path, token, body)
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("POST %s: %d: %s", path, rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}
	ws := create("/api/v1/workspaces", map[string]any{"name": "fixture"})
	sp := create("/api/v1/workspaces/"+itoa(ws)+"/spaces", map[string]any{"name": "fixture"})
	fo := create("/api/v1/spaces/"+itoa(sp)+"/folders", map[string]any{"name": "fixture"})
	li := create("/api/v1/spaces/"+itoa(sp)+"/lists", map[string]any{"name": "fixture"})
	st := create("/api/v1/lists/"+itoa(li)+"/statuses", map[string]any{"name": "Todo", "color": "#123456", "type": 1, "list_id": li})
	tg := create("/api/v1/spaces/"+itoa(sp)+"/tags", map[string]any{"name": "fixture", "color": "#654321"})
	tk := create("/api/v1/lists/"+itoa(li)+"/tasks", map[string]any{"title": "fixture", "status_id": st})
	cf := create("/api/v1/workspaces/"+itoa(ws)+"/custom-fields", map[string]any{
		"name": "fixture", "type": "text",
	})
	formSlug := fmt.Sprintf("route-fixture-%d", time.Now().UnixNano())
	form := create("/api/v1/workspaces/"+itoa(ws)+"/forms", map[string]any{
		"name": "fixture", "slug": formSlug,
	})
	question := create("/api/v1/workspaces/"+itoa(ws)+"/forms/"+itoa(form)+"/questions", map[string]any{
		"type": "short_text", "title": "fixture", "position": 0,
	})
	if rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/workspaces/"+itoa(ws)+"/forms/"+itoa(form)+"/publish", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("publish route fixture form: %d: %s", rec.Code, rec.Body.String())
	}
	submissionRec := requestWithHeader(t, tc, http.MethodPost,
		"/api/v1/public/forms/"+formSlug+"/submissions", "",
		"Idempotency-Key", fmt.Sprintf("route-fixture-%d", time.Now().UnixNano()),
		map[string]any{"answers": map[string]any{}})
	if submissionRec.Code != http.StatusCreated {
		t.Fatalf("create route fixture submission: %d: %s", submissionRec.Code, submissionRec.Body.String())
	}
	submission := responseID(t, submissionRec)
	template := create("/api/v1/workspaces/"+itoa(ws)+"/task-templates", map[string]any{
		"name": "fixture",
		"configuration": map[string]any{
			"task":          map[string]any{"priority": "normal"},
			"custom_fields": []any{}, "tags": []uint{}, "subtasks": []any{},
		},
	})
	templateVersionRec := request(t, tc.Router, http.MethodPost,
		"/api/v1/task-templates/"+itoa(template)+"/versions", token, map[string]any{
			"configuration": map[string]any{
				"task":          map[string]any{"priority": "normal"},
				"custom_fields": []any{}, "tags": []uint{}, "subtasks": []any{},
			},
		})
	if templateVersionRec.Code != http.StatusCreated {
		t.Fatalf("create route fixture template version: %d: %s", templateVersionRec.Code, templateVersionRec.Body.String())
	}
	templateVersion := responseID(t, templateVersionRec)
	cm := create("/api/v1/tasks/"+itoa(tk)+"/comments", map[string]any{"body": "fixture"})
	att := uploadFixtureFile(t, tc, token, tk)
	cl := create("/api/v1/tasks/"+itoa(tk)+"/checklists", map[string]any{"name": "fixture"})
	it := create("/api/v1/tasks/"+itoa(tk)+"/checklists/"+itoa(cl)+"/items", map[string]any{"text": "fixture"})

	return routeFixture{params: map[string]string{
		"workspace_id": itoa(ws), "space_id": itoa(sp), "folder_id": itoa(fo), "list_id": itoa(li),
		"status_id": itoa(st), "tag_id": itoa(tg), "task_id": itoa(tk), "comment_id": itoa(cm), "attachment_id": itoa(att), "checklist_id": itoa(cl), "item_id": itoa(it), "user_id": itoa(userID), "contact_id": itoa(userID), "notification_id": "1", "channel_id": "1", "view_id": "1", "entry_id": "1",
		"field_id": itoa(cf),
		"form_id":  itoa(form), "question_id": itoa(question), "submission_id": itoa(submission), "file_id": "1",
		"slug":        formSlug,
		"template_id": itoa(template), "version_id": itoa(templateVersion),
	}}
}

func (f routeFixture) expand(t *testing.T, path string) string {
	t.Helper()
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			v, ok := f.params[p[1:]]
			if !ok {
				t.Fatalf("route %s has parameter %s the fixture cannot fill; extend buildRouteFixture", path, p)
			}
			parts[i] = v
		}
	}
	return strings.Join(parts, "/")
}

func apiRoutes(t *testing.T, tc *TestContext) []gin.RouteInfo {
	t.Helper()
	engine, ok := tc.Raw.(*gin.Engine)
	if !ok {
		t.Fatalf("router is %T, want *gin.Engine", tc.Raw)
	}
	var routes []gin.RouteInfo
	for _, r := range engine.Routes() {
		if strings.HasPrefix(r.Path, "/api/v1/") {
			routes = append(routes, r)
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Path+routes[i].Method < routes[j].Path+routes[j].Method
	})
	if len(routes) < 20 {
		t.Fatalf("found only %d /api/v1 routes; route discovery is broken", len(routes))
	}
	return routes
}

func TestRoutes_EveryResourceRouteRejectsOutsiders(t *testing.T) {
	tc := newTestContext(t)
	ownerID, ownerToken := newUser(t, tc, fmt.Sprintf("route-owner-%d@example.test", time.Now().UnixNano()))
	fixture := buildRouteFixture(t, tc, ownerToken, ownerID)
	_, outsiderToken := newUser(t, tc, fmt.Sprintf("route-outsider-%d@example.test", time.Now().UnixNano()))

	for _, route := range apiRoutes(t, tc) {
		key := route.Method + " " + route.Path
		if _, ok := publicRoutes[key]; ok {
			continue
		}
		if _, ok := selfScopedRoutes[key]; ok {
			continue
		}
		t.Run(key, func(t *testing.T) {
			rec := request(t, tc.Router, route.Method, fixture.expand(t, route.Path), outsiderToken, nil)
			if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
				t.Fatalf("outsider got %d (want 403/404): %s\n"+
					"If this route is intentionally scoped to the caller, add it to selfScopedRoutes with a reason; "+
					"otherwise protect it with middleware.RequirePermission.", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoutes_ProtectedRoutesRequireAuthentication(t *testing.T) {
	tc := newTestContext(t)
	ownerID, ownerToken := newUser(t, tc, fmt.Sprintf("route-auth-%d@example.test", time.Now().UnixNano()))
	fixture := buildRouteFixture(t, tc, ownerToken, ownerID)

	for _, route := range apiRoutes(t, tc) {
		key := route.Method + " " + route.Path
		if _, ok := publicRoutes[key]; ok {
			continue
		}
		t.Run(key, func(t *testing.T) {
			rec := request(t, tc.Router, route.Method, fixture.expand(t, route.Path), "", nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous request got %d, want 401: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoutes_AllowListsReferenceExistingRoutes(t *testing.T) {
	tc := newTestContext(t)
	existing := map[string]bool{}
	for _, r := range apiRoutes(t, tc) {
		existing[r.Method+" "+r.Path] = true
	}
	for _, list := range []map[string]string{publicRoutes, selfScopedRoutes} {
		for key := range list {
			if !existing[key] {
				t.Errorf("allow-list entry %q does not match any registered route (stale entry or renamed route)", key)
			}
		}
	}
}
