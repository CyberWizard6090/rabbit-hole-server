package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"rabbit-hole-server/internal/persistence"
)

type sortFixture struct {
	id        uint
	title     string
	priority  int
	estimate  int
	spent     int
	statusPos int
	statusID  uint
	start     *time.Time
	due       *time.Time
	created   time.Time
	updated   time.Time
	tagIDs    []uint
	assignees []uint
	assignee  *string
}

type sortFixtureEnv struct {
	tc      *TestContext
	listURL string
	listID  uint
	tasks   []*sortFixture
	statusA uint
	statusB uint
	tagOne  uint
	tagTwo  uint
	userBob uint
	userZed uint
}

func setupSortFixture(t *testing.T) *sortFixtureEnv {
	t.Helper()
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	mkStatus := func(name string, position int) uint {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token, map[string]any{
			"name": name, "color": "#112233", "position": position, "type": 1, "list_id": listID,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status: %d %s", rec.Code, rec.Body.String())
		}
		return uint(dataObject(t, rec)["id"].(float64))
	}
	env := &sortFixtureEnv{tc: tc, listID: listID, listURL: "/api/v1/lists/" + itoa(listID) + "/tasks"}
	env.statusA = mkStatus("A", 2)
	env.statusB = mkStatus("B", 1)

	mkTag := func(name string) uint {
		tag := persistence.Tag{SpaceID: spaceID, Name: name, Color: "#112233"}
		if err := tc.DB.Create(&tag).Error; err != nil {
			t.Fatalf("create tag: %v", err)
		}
		return tag.ID
	}
	env.tagOne, env.tagTwo = mkTag("one"), mkTag("two")

	mkUser := func(first, last string) uint {
		u := persistence.User{
			Email: strings.ToLower(first) + "@sort.example.test", PasswordHash: "x",
			Username: "sort-" + strings.ToLower(first), FirstName: first, LastName: last,
		}
		if err := tc.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
		return u.ID
	}
	env.userBob, env.userZed = mkUser("Bob", "Smith"), mkUser("Zed", "Adams")
	names := map[uint]string{env.userBob: "bob smith", env.userZed: "zed adams"}

	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	day := func(n int) *time.Time {
		if n < 0 {
			return nil
		}
		d := base.AddDate(0, 0, n).Add(13 * time.Hour)
		return &d
	}
	type seed struct {
		title               string
		prio, est, spent    int
		status              uint
		start, due, created int
		tags, assignees     []uint
	}
	seeds := []seed{
		{"Alpha", 3, 30, 0, env.statusB, -1, 10, 0, []uint{env.tagOne}, []uint{env.userBob}},
		{"alpha", 1, 30, 5, env.statusA, -1, -1, 1, nil, nil},
		{"Bravo", 3, 0, 5, env.statusB, 1, 5, 2, []uint{env.tagOne, env.tagTwo}, []uint{env.userZed, env.userBob}},
		{"charlie", 5, 60, 0, env.statusA, 2, 5, 2, []uint{env.tagTwo}, []uint{env.userZed}},
		{"Delta", 1, 60, 10, env.statusB, -1, -1, 3, nil, nil},
		{"Echo foxtrot", 2, 15, 10, env.statusA, 3, 7, 4, []uint{env.tagOne}, nil},
		{"100% done", 2, 15, 0, env.statusB, 3, 8, 5, nil, []uint{env.userBob}},
		{"x_y", 4, 0, 0, env.statusA, -1, -1, 6, []uint{env.tagTwo}, nil},
		{"Echo", 3, 45, 15, env.statusB, 0, 5, 7, nil, nil},
	}
	var spaceStatuses = map[uint]int{env.statusA: 2, env.statusB: 1}
	var owner persistence.User
	if err := tc.DB.First(&owner, tc.UserID).Error; err != nil {
		t.Fatalf("owner: %v", err)
	}
	for i, s := range seeds {
		created := base.AddDate(0, 0, s.created).Add(time.Duration(i) * time.Minute)
		task := persistence.Task{
			SpaceID: spaceID, ListID: listID, StatusID: s.status, UserID: owner.ID,
			Title: s.title, Priority: s.prio, TimeEstimate: s.est, TimeSpent: s.spent,
			StartDate: day(s.start), DueDate: day(s.due),
		}
		task.CreatedAt = created
		task.UpdatedAt = base.AddDate(0, 0, 20).Add(time.Duration((i*5)%len(seeds)) * time.Hour)
		if err := tc.DB.Create(&task).Error; err != nil {
			t.Fatalf("create task %q: %v", s.title, err)
		}
		for _, tag := range s.tags {
			if err := tc.DB.Exec("INSERT INTO task_tags (task_id, tag_id) VALUES (?, ?)", task.ID, tag).Error; err != nil {
				t.Fatal(err)
			}
		}
		var lowest *string
		for _, u := range s.assignees {
			if err := tc.DB.Exec("INSERT INTO task_assignees (task_id, user_id) VALUES (?, ?)", task.ID, u).Error; err != nil {
				t.Fatal(err)
			}
			if lowest == nil || names[u] < *lowest {
				n := names[u]
				lowest = &n
			}
		}
		env.tasks = append(env.tasks, &sortFixture{
			id: task.ID, title: s.title, priority: s.prio, estimate: s.est, spent: s.spent,
			statusPos: spaceStatuses[s.status], statusID: s.status, start: task.StartDate, due: task.DueDate,
			created: task.CreatedAt, updated: task.UpdatedAt, tagIDs: s.tags, assignees: s.assignees, assignee: lowest,
		})
	}
	return env
}

func (f *sortFixture) value(field string) any {
	switch field {
	case "id":
		return int64(f.id)
	case "title":
		return strings.ToLower(f.title)
	case "status":
		return int64(f.statusPos)
	case "priority":
		return int64(f.priority)
	case "time_estimate":
		return int64(f.estimate)
	case "time_spent":
		return int64(f.spent)
	case "start_date":
		if f.start == nil {
			return nil
		}
		return *f.start
	case "due_date":
		if f.due == nil {
			return nil
		}
		return *f.due
	case "created_at":
		return f.created
	case "updated_at":
		return f.updated
	case "assignee":
		if f.assignee == nil {
			return nil
		}
		return *f.assignee
	}
	panic(field)
}

func compareValues(a, b any) int {
	switch av := a.(type) {
	case int64:
		return cmpInt(av, b.(int64))
	case string:
		return strings.Compare(av, b.(string))
	case time.Time:
		return av.Compare(b.(time.Time))
	}
	panic("unsupported value")
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func expectedOrder(tasks []*sortFixture, spec string) []uint {
	type key struct {
		field string
		desc  bool
	}
	var keys []key
	hasID := false
	for _, part := range strings.Split(spec, ",") {
		if part == "" {
			continue
		}
		k := key{field: strings.TrimPrefix(part, "-"), desc: strings.HasPrefix(part, "-")}
		hasID = hasID || k.field == "id"
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		keys, hasID = []key{{"id", true}}, true
	}
	if !hasID {
		keys = append(keys, key{"id", false})
	}
	sorted := append([]*sortFixture(nil), tasks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		for _, k := range keys {
			a, b := sorted[i].value(k.field), sorted[j].value(k.field)
			switch {
			case a == nil && b == nil:
				continue
			case a == nil:
				return false
			case b == nil:
				return true
			}
			c := compareValues(a, b)
			if c == 0 {
				continue
			}
			if k.desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
	ids := make([]uint, len(sorted))
	for i, f := range sorted {
		ids[i] = f.id
	}
	return ids
}

type listPage struct {
	ids        []uint
	next, prev string
	hasNext    bool
	hasPrev    bool
	total      *float64
}

func (e *sortFixtureEnv) get(t *testing.T, query url.Values) (*httptest.ResponseRecorder, listPage) {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, e.listURL+"?"+query.Encode(), e.tc.Token, nil)
	var page listPage
	if rec.Code != http.StatusOK {
		return rec, page
	}
	data := dataObject(t, rec)
	for _, raw := range data["items"].([]any) {
		page.ids = append(page.ids, uint(raw.(map[string]any)["id"].(float64)))
	}
	page.next, _ = data["next_cursor"].(string)
	page.prev, _ = data["prev_cursor"].(string)
	page.hasNext, _ = data["has_next"].(bool)
	page.hasPrev, _ = data["has_prev"].(bool)
	if v, ok := data["total_count"].(float64); ok {
		page.total = &v
	}
	return rec, page
}

func (e *sortFixtureEnv) walk(t *testing.T, base url.Values, limit int) []uint {
	t.Helper()
	q := cloneValues(base)
	q.Set("limit", fmt.Sprint(limit))

	var forward [][]uint
	rec, page := e.get(t, q)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", q.Encode(), rec.Code, rec.Body.String())
	}
	forward = append(forward, page.ids)
	if page.hasPrev || page.prev != "" {
		t.Fatalf("first page must not have prev: %+v", page)
	}
	for guard := 0; page.hasNext; guard++ {
		if guard > 50 || page.next == "" {
			t.Fatalf("broken forward pagination: %+v", page)
		}
		q.Set("cursor", page.next)
		if rec, page = e.get(t, q); rec.Code != http.StatusOK {
			t.Fatalf("next page: %d %s", rec.Code, rec.Body.String())
		}
		if !page.hasPrev || page.prev == "" {
			t.Fatalf("non-first page must have prev: %+v", page)
		}
		forward = append(forward, page.ids)
	}

	back := [][]uint{page.ids}
	for guard := 0; page.hasPrev; guard++ {
		if guard > 50 {
			t.Fatal("broken backward pagination")
		}
		q.Set("cursor", page.prev)
		if rec, page = e.get(t, q); rec.Code != http.StatusOK {
			t.Fatalf("prev page: %d %s", rec.Code, rec.Body.String())
		}
		back = append(back, page.ids)
	}
	for i, j := 0, len(back)-1; i < j; i, j = i+1, j-1 {
		back[i], back[j] = back[j], back[i]
	}
	if !reflect.DeepEqual(forward, back) {
		t.Fatalf("backward walk %v != forward walk %v (query %s)", back, forward, base.Encode())
	}
	var all []uint
	for _, p := range forward {
		all = append(all, p...)
	}
	return all
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func TestTaskListSortEveryFieldPostgres(t *testing.T) {
	env := setupSortFixture(t)
	fields := []string{"id", "title", "status", "priority", "time_estimate", "time_spent",
		"start_date", "due_date", "created_at", "updated_at", "assignee"}
	for _, field := range fields {
		for _, spec := range []string{field, "-" + field} {
			t.Run(spec, func(t *testing.T) {
				got := env.walk(t, url.Values{"sort": {spec}}, 4)
				want := expectedOrder(env.tasks, spec)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("sort=%s\n got %v\nwant %v", spec, got, want)
				}
			})
		}
	}
}

func TestTaskListSortCombinationsAndDefaultPostgres(t *testing.T) {
	env := setupSortFixture(t)
	if got, want := env.walk(t, url.Values{}, 4), expectedOrder(env.tasks, ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("default order\n got %v\nwant %v", got, want)
	}
	for _, spec := range []string{
		"-priority,due_date",
		"priority,-due_date,title",
		"-due_date,-start_date",
		"assignee,-priority",
		"status,-assignee,start_date",
		"-time_spent,id",
	} {
		for _, limit := range []int{1, 2, 4} {
			got := env.walk(t, url.Values{"sort": {spec}}, limit)
			if want := expectedOrder(env.tasks, spec); !reflect.DeepEqual(got, want) {
				t.Fatalf("sort=%s limit=%d\n got %v\nwant %v", spec, limit, got, want)
			}
		}
	}
}

func TestTaskListNullsAreAlwaysLastPostgres(t *testing.T) {
	env := setupSortFixture(t)
	for _, spec := range []string{"due_date", "-due_date"} {
		_, page := env.get(t, url.Values{"sort": {spec}, "limit": {"100"}})
		seenNull := false
		for _, id := range page.ids {
			isNull := env.byID(id).due == nil
			if seenNull && !isNull {
				t.Fatalf("sort=%s: non-null due_date after NULL: %v", spec, page.ids)
			}
			seenNull = seenNull || isNull
		}
		if !seenNull {
			t.Fatal("fixture should contain tasks without due date")
		}
	}
}

func (e *sortFixtureEnv) byID(id uint) *sortFixture {
	for _, f := range e.tasks {
		if f.id == id {
			return f
		}
	}
	return nil
}

func TestTaskListFiltersPostgres(t *testing.T) {
	env := setupSortFixture(t)
	idsWhere := func(pred func(*sortFixture) bool) []uint {
		var ids []uint
		for _, f := range env.tasks {
			if pred(f) {
				ids = append(ids, f.id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids
	}
	hasTag := func(f *sortFixture, tags ...uint) bool {
		for _, have := range f.tagIDs {
			for _, want := range tags {
				if have == want {
					return true
				}
			}
		}
		return false
	}
	hasAssignee := func(f *sortFixture, users ...uint) bool {
		for _, have := range f.assignees {
			for _, want := range users {
				if have == want {
					return true
				}
			}
		}
		return false
	}
	inDay := func(ts *time.Time, from, to string) bool {
		if ts == nil {
			return false
		}
		d := ts.UTC().Format("2006-01-02")
		return (from == "" || d >= from) && (to == "" || d <= to)
	}
	titleHas := func(f *sortFixture, s string) bool {
		return strings.Contains(strings.ToLower(f.title), strings.ToLower(s))
	}

	cases := []struct {
		name  string
		query url.Values
		want  []uint
	}{
		{"q case-insensitive", url.Values{"q": {"ALPHA"}}, idsWhere(func(f *sortFixture) bool { return titleHas(f, "alpha") })},
		{"q percent is literal", url.Values{"q": {"%"}}, idsWhere(func(f *sortFixture) bool { return titleHas(f, "%") })},
		{"q underscore is literal", url.Values{"q": {"_"}}, idsWhere(func(f *sortFixture) bool { return titleHas(f, "_") })},
		{"status_id", url.Values{"status_id": {fmt.Sprint(env.statusA)}}, idsWhere(func(f *sortFixture) bool { return f.statusID == env.statusA })},
		{"status_id list", url.Values{"status_id": {fmt.Sprintf("%d,%d", env.statusA, env.statusB)}}, idsWhere(func(*sortFixture) bool { return true })},
		{"status_id outside list matches nothing", url.Values{"status_id": {"999999"}}, nil},
		{"priority", url.Values{"priority": {"1,2"}}, idsWhere(func(f *sortFixture) bool { return f.priority <= 2 })},
		{"tag_id any", url.Values{"tag_id": {fmt.Sprintf("%d,%d", env.tagOne, env.tagTwo)}}, idsWhere(func(f *sortFixture) bool { return hasTag(f, env.tagOne, env.tagTwo) })},
		{"tag_id single", url.Values{"tag_id": {fmt.Sprint(env.tagTwo)}}, idsWhere(func(f *sortFixture) bool { return hasTag(f, env.tagTwo) })},
		{"no_tags", url.Values{"no_tags": {"true"}}, idsWhere(func(f *sortFixture) bool { return len(f.tagIDs) == 0 })},
		{"no_tags or tag_id", url.Values{"no_tags": {"true"}, "tag_id": {fmt.Sprint(env.tagOne)}}, idsWhere(func(f *sortFixture) bool { return len(f.tagIDs) == 0 || hasTag(f, env.tagOne) })},
		{"assignee_id", url.Values{"assignee_id": {fmt.Sprint(env.userBob)}}, idsWhere(func(f *sortFixture) bool { return hasAssignee(f, env.userBob) })},
		{"unassigned", url.Values{"unassigned": {"true"}}, idsWhere(func(f *sortFixture) bool { return len(f.assignees) == 0 })},
		{"unassigned or assignee_id", url.Values{"unassigned": {"true"}, "assignee_id": {fmt.Sprint(env.userZed)}}, idsWhere(func(f *sortFixture) bool { return len(f.assignees) == 0 || hasAssignee(f, env.userZed) })},
		{"created range inclusive", url.Values{"created_from": {"2026-11-02"}, "created_to": {"2026-11-03"}}, idsWhere(func(f *sortFixture) bool { return inDay(&f.created, "2026-11-02", "2026-11-03") })},
		{"due range excludes null", url.Values{"due_from": {"2026-11-06"}, "due_to": {"2026-11-09"}}, idsWhere(func(f *sortFixture) bool { return inDay(f.due, "2026-11-06", "2026-11-09") })},
		{"due_to is inclusive of whole day", url.Values{"due_to": {"2026-11-06"}}, idsWhere(func(f *sortFixture) bool { return inDay(f.due, "", "2026-11-06") })},
		{"start range", url.Values{"start_from": {"2026-11-02"}}, idsWhere(func(f *sortFixture) bool { return inDay(f.start, "2026-11-02", "") })},
		{"has_due true", url.Values{"has_due": {"true"}}, idsWhere(func(f *sortFixture) bool { return f.due != nil })},
		{"has_due false", url.Values{"has_due": {"false"}}, idsWhere(func(f *sortFixture) bool { return f.due == nil })},
		{"combined AND", url.Values{"priority": {"3"}, "has_due": {"true"}, "tag_id": {fmt.Sprint(env.tagOne)}, "q": {"a"}},
			idsWhere(func(f *sortFixture) bool {
				return f.priority == 3 && f.due != nil && hasTag(f, env.tagOne) && titleHas(f, "a")
			})},
		{"empty params are ignored", url.Values{"q": {""}, "priority": {""}, "has_due": {""}}, idsWhere(func(*sortFixture) bool { return true })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := cloneValues(tc.query)
			q.Set("sort", "id")
			got := env.walk(t, q, 3)
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("filter %s\n got %v\nwant %v", tc.query.Encode(), got, tc.want)
			}
		})
	}
}

func TestTaskListFilterWithSortAndTotalPostgres(t *testing.T) {
	env := setupSortFixture(t)
	q := url.Values{"priority": {"3,5"}, "sort": {"-due_date,title"}, "with_total": {"true"}, "limit": {"2"}}
	_, page := env.get(t, q)
	if page.total == nil || int(*page.total) != 4 {
		t.Fatalf("total_count = %v, want 4", page.total)
	}
	q.Set("cursor", page.next)
	if _, second := env.get(t, q); second.total == nil || int(*second.total) != 4 {
		t.Fatalf("total_count on page 2 = %v, want 4", second.total)
	}
	if _, plain := env.get(t, url.Values{"priority": {"3,5"}}); plain.total != nil {
		t.Fatalf("total_count must be omitted without with_total, got %v", *plain.total)
	}
	got := env.walk(t, url.Values{"priority": {"3,5"}, "sort": {"-due_date,title"}}, 2)
	var filtered []*sortFixture
	for _, f := range env.tasks {
		if f.priority == 3 || f.priority == 5 {
			filtered = append(filtered, f)
		}
	}
	if want := expectedOrder(filtered, "-due_date,title"); !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered+sorted\n got %v\nwant %v", got, want)
	}
}

func TestTaskListCursorBoundToSortAndFiltersPostgres(t *testing.T) {
	env := setupSortFixture(t)
	_, page := env.get(t, url.Values{"sort": {"priority"}, "limit": {"2"}, "q": {"a"}, "status_id": {fmt.Sprintf("%d,%d", env.statusA, env.statusB)}})
	if page.next == "" {
		t.Fatal("expected next cursor")
	}
	base := func() url.Values {
		return url.Values{"sort": {"priority"}, "limit": {"2"}, "q": {"a"}, "cursor": {page.next}, "status_id": {fmt.Sprintf("%d,%d", env.statusA, env.statusB)}}
	}
	mutate := map[string]func(url.Values){
		"other sort":      func(v url.Values) { v.Set("sort", "-priority") },
		"sort dropped":    func(v url.Values) { v.Del("sort") },
		"other sort key":  func(v url.Values) { v.Set("sort", "priority,title") },
		"other q":         func(v url.Values) { v.Set("q", "b") },
		"filter dropped":  func(v url.Values) { v.Del("q") },
		"filter added":    func(v url.Values) { v.Set("has_due", "true") },
		"other status_id": func(v url.Values) { v.Set("status_id", fmt.Sprint(env.statusA)) },
	}
	for name, change := range mutate {
		t.Run(name, func(t *testing.T) {
			q := base()
			change(q)
			rec, _ := env.get(t, q)
			assertErrorCode(t, rec, http.StatusBadRequest, "INVALID_PAGINATION")
		})
	}

	t.Run("equivalent filters keep the cursor valid", func(t *testing.T) {
		q := base()
		q.Set("status_id", fmt.Sprintf("%d,%d,%d", env.statusB, env.statusA, env.statusB))
		q.Set("with_total", "true")
		q.Set("q", "  a ")
		rec, _ := env.get(t, q)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cursor from another list", func(t *testing.T) {
		other := "/api/v1/lists/" + itoa(env.listID+1000) + "/tasks?" + base().Encode()
		rec := request(t, env.tc.Router, http.MethodGet, other, env.tc.Token, nil)
		if rec.Code == http.StatusOK {
			t.Fatalf("cursor accepted for another list: %s", rec.Body.String())
		}
	})
}

func TestTaskListInvalidSortAndFilterPostgres(t *testing.T) {
	env := setupSortFixture(t)
	cases := []struct {
		name  string
		query url.Values
		code  string
		field string
	}{
		{"unknown sort field", url.Values{"sort": {"description"}}, "INVALID_SORT", "sort"},
		{"sql injection in sort", url.Values{"sort": {"id;drop table tasks"}}, "INVALID_SORT", "sort"},
		{"too many sort fields", url.Values{"sort": {"id,title,priority,status"}}, "INVALID_SORT", "sort"},
		{"duplicate sort field", url.Values{"sort": {"priority,-priority"}}, "INVALID_SORT", "sort"},
		{"empty sort item", url.Values{"sort": {"priority,"}}, "INVALID_SORT", "sort"},
		{"bad status_id", url.Values{"status_id": {"a"}}, "INVALID_FILTER", "status_id"},
		{"zero tag_id", url.Values{"tag_id": {"0"}}, "INVALID_FILTER", "tag_id"},
		{"priority out of range", url.Values{"priority": {"6"}}, "INVALID_FILTER", "priority"},
		{"bad assignee_id", url.Values{"assignee_id": {"1,,2"}}, "INVALID_FILTER", "assignee_id"},
		{"bad bool", url.Values{"no_tags": {"yes"}}, "INVALID_FILTER", "no_tags"},
		{"bad has_due", url.Values{"has_due": {"maybe"}}, "INVALID_FILTER", "has_due"},
		{"bad date format", url.Values{"created_from": {"01.11.2026"}}, "INVALID_FILTER", "created_from"},
		{"impossible date", url.Values{"due_to": {"2026-02-30"}}, "INVALID_FILTER", "due_to"},
		{"reversed range", url.Values{"start_from": {"2026-11-05"}, "start_to": {"2026-11-01"}}, "INVALID_FILTER", "start_from"},
		{"bad with_total", url.Values{"with_total": {"1x"}}, "INVALID_FILTER", "with_total"},
		{"too long q", url.Values{"q": {strings.Repeat("a", 201)}}, "INVALID_FILTER", "q"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := env.get(t, tc.query)
			assertErrorCode(t, rec, http.StatusBadRequest, tc.code)
			fields, _ := decodeJSON(t, rec)["error"].(map[string]any)["fields"].(map[string]any)
			if _, ok := fields[tc.field]; !ok {
				t.Fatalf("error fields = %v, want key %q", fields, tc.field)
			}
		})
	}
}

func TestTaskListTieBreakByIDAcrossPagesPostgres(t *testing.T) {
	env := setupSortFixture(t)
	got := env.walk(t, url.Values{"sort": {"priority"}}, 1)
	if want := expectedOrder(env.tasks, "priority"); !reflect.DeepEqual(got, want) {
		t.Fatalf("tie-break\n got %v\nwant %v", got, want)
	}
	seen := map[uint]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("task %d returned twice: %v", id, got)
		}
		seen[id] = true
	}
	if len(seen) != len(env.tasks) {
		t.Fatalf("returned %d tasks, want %d", len(seen), len(env.tasks))
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	errBody, _ := decodeJSON(t, rec)["error"].(map[string]any)
	if errBody["code"] != code {
		t.Fatalf("error code = %v, want %s: %s", errBody["code"], code, rec.Body.String())
	}
}
