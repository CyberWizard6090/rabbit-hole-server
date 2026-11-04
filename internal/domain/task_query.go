package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/http/pagination"
)

type TaskSortField string

const (
	TaskSortID           TaskSortField = "id"
	TaskSortTitle        TaskSortField = "title"
	TaskSortStatus       TaskSortField = "status"
	TaskSortPriority     TaskSortField = "priority"
	TaskSortTimeEstimate TaskSortField = "time_estimate"
	TaskSortTimeSpent    TaskSortField = "time_spent"
	TaskSortStartDate    TaskSortField = "start_date"
	TaskSortDueDate      TaskSortField = "due_date"
	TaskSortCreatedAt    TaskSortField = "created_at"
	TaskSortUpdatedAt    TaskSortField = "updated_at"
	TaskSortAssignee     TaskSortField = "assignee"
	TaskSortPosition     TaskSortField = "position"
)

const MaxTaskSortKeys = 3

const TaskSortTextLimit = 100

var taskSortFields = map[TaskSortField]struct{}{
	TaskSortID: {}, TaskSortTitle: {}, TaskSortStatus: {}, TaskSortPriority: {},
	TaskSortTimeEstimate: {}, TaskSortTimeSpent: {}, TaskSortStartDate: {},
	TaskSortDueDate: {}, TaskSortCreatedAt: {}, TaskSortUpdatedAt: {}, TaskSortAssignee: {},
	TaskSortPosition: {},
}

func IsValidTaskSortField(field TaskSortField) bool {
	_, ok := taskSortFields[field]
	return ok
}

type TaskSortKey struct {
	Field TaskSortField
	Desc  bool
}

type TaskFilter struct {
	Query       string
	StatusIDs   []uint
	Priorities  []int
	TagIDs      []uint
	NoTags      bool
	AssigneeIDs []uint
	Unassigned  bool
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	DueFrom     *time.Time
	DueTo       *time.Time
	StartFrom   *time.Time
	StartTo     *time.Time
	HasDue      *bool
}

type TaskQuery struct {
	Sort      []TaskSortKey
	Filter    TaskFilter
	WithTotal bool
}

func (q TaskQuery) SortKeys() []TaskSortKey {
	if len(q.Sort) == 0 {
		return []TaskSortKey{{Field: TaskSortID, Desc: true}}
	}
	keys := append([]TaskSortKey(nil), q.Sort...)
	for _, key := range keys {
		if key.Field == TaskSortID {
			return keys
		}
	}
	return append(keys, TaskSortKey{Field: TaskSortID})
}

func (q TaskQuery) SortSignature() string {
	keys := q.SortKeys()
	parts := make([]string, len(keys))
	for i, key := range keys {
		dir := "asc"
		if key.Desc {
			dir = "desc"
		}
		parts[i] = string(key.Field) + "_" + dir
	}
	return strings.Join(parts, ",")
}

func (q TaskQuery) FilterSignature() string {
	f := q.Filter
	var b strings.Builder
	add := func(name, value string) {
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteByte(';')
	}
	if f.Query != "" {
		add("q", f.Query)
	}
	if len(f.StatusIDs) > 0 {
		add("status", joinUints(f.StatusIDs))
	}
	if len(f.Priorities) > 0 {
		ints := append([]int(nil), f.Priorities...)
		sort.Ints(ints)
		parts := make([]string, len(ints))
		for i, v := range ints {
			parts[i] = strconv.Itoa(v)
		}
		add("priority", strings.Join(parts, ","))
	}
	if len(f.TagIDs) > 0 {
		add("tag", joinUints(f.TagIDs))
	}
	if f.NoTags {
		add("no_tags", "1")
	}
	if len(f.AssigneeIDs) > 0 {
		add("assignee", joinUints(f.AssigneeIDs))
	}
	if f.Unassigned {
		add("unassigned", "1")
	}
	for _, d := range []struct {
		name string
		t    *time.Time
	}{
		{"created_from", f.CreatedFrom}, {"created_to", f.CreatedTo},
		{"due_from", f.DueFrom}, {"due_to", f.DueTo},
		{"start_from", f.StartFrom}, {"start_to", f.StartTo},
	} {
		if d.t != nil {
			add(d.name, d.t.UTC().Format("2006-01-02"))
		}
	}
	if f.HasDue != nil {
		add("has_due", strconv.FormatBool(*f.HasDue))
	}
	if b.Len() == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

func joinUints(ids []uint) string {
	sorted := append([]uint(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ",")
}

func (q TaskQuery) CursorContext(listID uint) pagination.CursorContext {
	return pagination.TaskCursorContext(listID, q.SortSignature(), q.FilterSignature())
}

func (t Task) SortValue(field TaskSortField) any {
	switch field {
	case TaskSortID:
		return int64(t.ID) //nolint:gosec // ids are positive database serials
	case TaskSortTitle:
		return truncateRunes(t.Title, TaskSortTextLimit)
	case TaskSortStatus:
		return int64(t.SortStatusPosition)
	case TaskSortPriority:
		return int64(t.Priority)
	case TaskSortTimeEstimate:
		return int64(t.TimeEstimate)
	case TaskSortTimeSpent:
		return int64(t.TimeSpent)
	case TaskSortPosition:
		return t.Position
	case TaskSortStartDate:
		return timeOrNil(t.StartDate)
	case TaskSortDueDate:
		return timeOrNil(t.DueDate)
	case TaskSortCreatedAt:
		return t.CreatedAt
	case TaskSortUpdatedAt:
		return t.UpdatedAt
	case TaskSortAssignee:
		if t.SortAssignee == nil {
			return nil
		}
		return *t.SortAssignee
	}
	panic(fmt.Sprintf("domain: unknown task sort field %q", field))
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
