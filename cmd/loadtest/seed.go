package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"gorm.io/gorm"
)

type environment struct {
	db     *gorm.DB
	base   string
	client *http.Client

	tokens      []string
	userIDs     []uint
	workspaceID uint
	listIDs     []uint
	statuses    map[uint][]uint
	taskIDs     []uint
	words       []string
	formSlug    string
	questionID  uint
}

const password = "password-123"

func (e *environment) call(method, path, token string, body any, headers map[string]string) (int, []byte, time.Duration, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, 0, err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.base+path, payload)
	if err != nil {
		return 0, nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.%d.%d.%d", rand.Intn(250)+1, rand.Intn(250)+1, rand.Intn(250)+1))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	started := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		return 0, nil, time.Since(started), err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, time.Since(started), err
}

func (e *environment) mustCall(method, path, token string, body any, want int) (map[string]any, error) {
	status, data, _, err := e.call(method, path, token, body, nil)
	if err != nil {
		return nil, err
	}
	if status != want {
		return nil, fmt.Errorf("%s %s: %d: %s", method, path, status, string(data))
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(data, &out)
	return out.Data, nil
}

func idOf(m map[string]any) uint {
	v, _ := m["id"].(float64)
	return uint(v)
}

func (e *environment) login(i int) (string, error) {
	data, err := e.mustCall(http.MethodPost, "/auth/login", "", map[string]any{
		"email": fmt.Sprintf("load-%d@example.test", i), "password": password,
	}, http.StatusOK)
	if err != nil {
		return "", err
	}
	token, _ := data["access_token"].(string)
	return token, nil
}

func (e *environment) seed(o options) error {
	for i := 0; i < o.users; i++ {
		if _, err := e.mustCall(http.MethodPost, "/auth/register", "", map[string]any{
			"email": fmt.Sprintf("load-%d@example.test", i), "password": password,
		}, http.StatusCreated); err != nil {
			return err
		}
	}
	if err := e.reload(o); err != nil {
		return err
	}
	owner := e.tokens[0]
	ws, err := e.mustCall(http.MethodPost, "/workspaces", owner, map[string]any{"name": "Load workspace"}, http.StatusCreated)
	if err != nil {
		return err
	}
	e.workspaceID = idOf(ws)
	for s := 0; s < 3; s++ {
		space, err := e.mustCall(http.MethodPost, fmt.Sprintf("/workspaces/%d/spaces", e.workspaceID), owner,
			map[string]any{"name": fmt.Sprintf("Space %d", s), "template": "kanban"}, http.StatusCreated)
		if err != nil {
			return err
		}
		for l := 0; l < 3; l++ {
			if _, err := e.mustCall(http.MethodPost, fmt.Sprintf("/spaces/%d/lists", idOf(space)), owner,
				map[string]any{"name": fmt.Sprintf("List %d.%d", s, l), "template": "development"}, http.StatusCreated); err != nil {
				return err
			}
		}
	}
	if err := execAll(e.db, fmt.Sprintf(`
		INSERT INTO workspace_members (workspace_id, user_id, role_id)
		SELECT %[1]d, u.id, r.id FROM users u JOIN roles r ON r.workspace_id = %[1]d AND r.name = 'Member'
		WHERE u.id <> %[2]d`, e.workspaceID, e.userIDs[0])); err != nil {
		return err
	}
	if err := e.bulk(o); err != nil {
		return err
	}
	form, err := e.mustCall(http.MethodPost, fmt.Sprintf("/workspaces/%d/forms", e.workspaceID), owner,
		map[string]any{"name": "Load form", "slug": "load-form"}, http.StatusCreated)
	if err != nil {
		return err
	}
	question, err := e.mustCall(http.MethodPost, fmt.Sprintf("/workspaces/%d/forms/%d/questions", e.workspaceID, idOf(form)), owner,
		map[string]any{"type": "short_text", "title": "Summary", "required": true, "position": 0}, http.StatusCreated)
	if err != nil {
		return err
	}
	if _, err := e.mustCall(http.MethodPost, fmt.Sprintf("/workspaces/%d/forms/%d/publish", e.workspaceID, idOf(form)), owner, nil, http.StatusOK); err != nil {
		return err
	}
	e.formSlug, e.questionID = "load-form", idOf(question)
	return e.reload(o)
}

func (e *environment) bulk(o options) error {
	var lists int64
	if err := e.db.Raw("SELECT count(*) FROM lists WHERE deleted_at IS NULL").Scan(&lists).Error; err != nil {
		return err
	}
	perList := o.tasks / int(lists)
	owner := e.userIDs[0]
	return execAll(e.db,
		fmt.Sprintf(`
		INSERT INTO tasks (space_id, list_id, status_id, title, description, priority, user_id, due_date, position, created_at)
		SELECT l.space_id, l.id, s.id,
		       (ARRAY['Deploy','Invoice','Refactor','Customer','Report','Login','Search','Release'])[1 + g %% 8] || ' ' ||
		       left(md5(g::text || l.id::text), 7) || ' ' || left(md5(l.id::text || g::text), 7),
		       'Description of task ' || g || ' ' || md5(g::text), 1 + g %% 5, %d,
		       CASE WHEN g %% 3 = 0 THEN now() + ((g %% 90) || ' days')::interval END, g,
		       now() - (g || ' seconds')::interval
		FROM lists l
		CROSS JOIN LATERAL generate_series(1, %d) g
		JOIN LATERAL (SELECT id FROM task_statuses WHERE list_id = l.id AND deleted_at IS NULL ORDER BY position OFFSET (g %% 5) LIMIT 1) s ON true
		WHERE l.deleted_at IS NULL`, owner, perList),
		fmt.Sprintf(`
		INSERT INTO task_assignees (task_id, user_id)
		SELECT t.id, (ARRAY[%s])[1 + (t.id %% %d)] FROM tasks t WHERE t.id %% 3 = 0`, joinIDs(e.userIDs), len(e.userIDs)),
		fmt.Sprintf(`
		INSERT INTO task_comments (task_id, user_id, body, created_at)
		SELECT t.id, %d, 'Comment ' || t.id || ' ' || md5(t.id::text), now() - (t.id %% 1000 || ' minutes')::interval
		FROM tasks t WHERE t.id %% 4 = 0`, owner),
		fmt.Sprintf(`
		INSERT INTO task_activities (task_id, user_id, actor_type, action, field, created_at)
		SELECT t.id, %d, 'user', 'task.updated', 'priority', now() - (t.id %% 5000 || ' minutes')::interval
		FROM tasks t WHERE t.id %% 2 = 0`, owner),
		fmt.Sprintf(`
		INSERT INTO notifications (user_id, type, task_id, actor_id, workspace_id, payload, created_at, read_at)
		SELECT u.id, 'comment', t.id, %d, %d, '{}', now() - (t.id %% 10000 || ' minutes')::interval,
		       CASE WHEN t.id %% 3 = 0 THEN now() END
		FROM users u JOIN tasks t ON t.id %% 2000 = u.id %% 2000`, owner, e.workspaceID),
		"ANALYZE",
	)
}

func joinIDs(ids []uint) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprint(id)
	}
	return out
}

func (e *environment) reload(o options) error {
	e.tokens, e.userIDs = nil, nil
	for i := 0; i < o.users; i++ {
		token, err := e.login(i)
		if err != nil {
			return err
		}
		e.tokens = append(e.tokens, token)
		var id uint
		if err := e.db.Raw("SELECT id FROM users WHERE email = ?", fmt.Sprintf("load-%d@example.test", i)).Scan(&id).Error; err != nil {
			return err
		}
		e.userIDs = append(e.userIDs, id)
	}
	e.db.Raw("SELECT id FROM workspaces ORDER BY id LIMIT 1").Scan(&e.workspaceID)
	e.db.Raw("SELECT id FROM lists WHERE deleted_at IS NULL ORDER BY id").Scan(&e.listIDs)
	e.db.Raw("SELECT id FROM tasks WHERE deleted_at IS NULL ORDER BY random() LIMIT 5000").Scan(&e.taskIDs)
	e.db.Raw("SELECT split_part(title, ' ', 2) FROM tasks WHERE deleted_at IS NULL ORDER BY random() LIMIT 500").Scan(&e.words)
	e.statuses = map[uint][]uint{}
	type row struct{ ListID, ID uint }
	var rows []row
	e.db.Raw("SELECT list_id, id FROM task_statuses WHERE deleted_at IS NULL").Scan(&rows)
	for _, r := range rows {
		e.statuses[r.ListID] = append(e.statuses[r.ListID], r.ID)
	}
	if e.formSlug == "" {
		var slug string
		e.db.Raw("SELECT slug FROM forms ORDER BY id LIMIT 1").Scan(&slug)
		e.formSlug = slug
		e.db.Raw("SELECT id FROM form_questions ORDER BY id LIMIT 1").Scan(&e.questionID)
	}
	return nil
}
