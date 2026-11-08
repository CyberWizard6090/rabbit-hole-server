package taskquery

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
)

const (
	CodeInvalidSort   = "INVALID_SORT"
	CodeInvalidFilter = "INVALID_FILTER"

	maxSearchLength = 200
	maxListValues   = 100
	dateLayout      = "2006-01-02"
)

func Parse(values url.Values) (domain.TaskQuery, *httperrors.AppError) {
	var query domain.TaskQuery

	sortKeys, sortErr := parseSort(values.Get("sort"))
	if sortErr != nil {
		return query, &httperrors.AppError{
			Code:    CodeInvalidSort,
			Message: "invalid sort",
			Status:  http.StatusBadRequest,
			Details: []httperrors.Detail{*sortErr},
		}
	}
	query.Sort = sortKeys

	p := &filterParser{values: values}
	f := &query.Filter

	if q := strings.TrimSpace(values.Get("q")); q != "" {
		if utf8.RuneCountInString(q) > maxSearchLength {
			p.fail("q", "TOO_LONG", map[string]any{"max": maxSearchLength})
		} else {
			f.Query = q
		}
	}
	f.StatusIDs = p.ids("status_id")
	f.Priorities = p.priorities("priority")
	f.TagIDs = p.ids("tag_id")
	f.NoTags = p.boolean("no_tags")
	f.AssigneeIDs = p.ids("assignee_id")
	f.Unassigned = p.boolean("unassigned")
	f.CreatedFrom, f.CreatedTo = p.dateRange("created_from", "created_to")
	f.DueFrom, f.DueTo = p.dateRange("due_from", "due_to")
	f.StartFrom, f.StartTo = p.dateRange("start_from", "start_to")
	f.HasDue = p.optionalBool("has_due")
	f.HasOpenChecklistItems = p.optionalBool("has_open_checklist_items")
	query.WithTotal = p.boolean("with_total")

	if len(p.details) > 0 {
		return query, &httperrors.AppError{
			Code:    CodeInvalidFilter,
			Message: "invalid filter parameters",
			Status:  http.StatusBadRequest,
			Details: p.details,
		}
	}
	return query, nil
}

func parseSort(raw string) ([]domain.TaskSortKey, *httperrors.Detail) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	fail := func(code string, params map[string]any) *httperrors.Detail {
		return &httperrors.Detail{Field: "sort", Code: code, Params: params}
	}
	parts := strings.Split(raw, ",")
	if len(parts) > domain.MaxTaskSortKeys {
		return nil, fail("TOO_MANY", map[string]any{"max": domain.MaxTaskSortKeys})
	}
	seen := map[domain.TaskSortField]struct{}{}
	keys := make([]domain.TaskSortKey, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		desc := strings.HasPrefix(part, "-")
		field := domain.TaskSortField(strings.TrimPrefix(part, "-"))
		if !domain.IsValidTaskSortField(field) {
			return nil, fail("INVALID_CHOICE", map[string]any{"value": string(field)})
		}
		if _, dup := seen[field]; dup {
			return nil, fail("DUPLICATE", map[string]any{"value": string(field)})
		}
		seen[field] = struct{}{}
		keys = append(keys, domain.TaskSortKey{Field: field, Desc: desc})
	}
	return keys, nil
}

type filterParser struct {
	values  url.Values
	details []httperrors.Detail
}

// fail records the first problem found for a query parameter.
func (p *filterParser) fail(name, code string, params map[string]any) {
	for _, d := range p.details {
		if d.Field == name {
			return
		}
	}
	p.details = append(p.details, httperrors.Detail{Field: name, Code: code, Params: params})
}

func (p *filterParser) list(name string) ([]string, bool) {
	raw, present := p.values[name]
	if !present {
		return nil, false
	}
	var items []string
	for _, chunk := range raw {
		for _, item := range strings.Split(chunk, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				if strings.TrimSpace(chunk) == "" {
					continue
				}
				p.fail(name, "INVALID_VALUE", nil)
				return nil, false
			}
			items = append(items, item)
		}
	}
	if len(items) > maxListValues {
		p.fail(name, "TOO_MANY", map[string]any{"max": maxListValues})
		return nil, false
	}
	return items, len(items) > 0
}

func (p *filterParser) ids(name string) []uint {
	items, ok := p.list(name)
	if !ok {
		return nil
	}
	seen := map[uint]struct{}{}
	result := make([]uint, 0, len(items))
	for _, item := range items {
		n, err := strconv.ParseUint(item, 10, 63)
		if err != nil || n == 0 {
			p.fail(name, "INVALID_VALUE", nil)
			return nil
		}
		if _, dup := seen[uint(n)]; !dup {
			seen[uint(n)] = struct{}{}
			result = append(result, uint(n))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (p *filterParser) priorities(name string) []int {
	items, ok := p.list(name)
	if !ok {
		return nil
	}
	seen := map[int]struct{}{}
	result := make([]int, 0, len(items))
	for _, item := range items {
		n, err := strconv.Atoi(item)
		if err != nil || n < 1 || n > 5 {
			p.fail(name, "INVALID_RANGE", map[string]any{"min": 1, "max": 5})
			return nil
		}
		if _, dup := seen[n]; !dup {
			seen[n] = struct{}{}
			result = append(result, n)
		}
	}
	sort.Ints(result)
	return result
}

func (p *filterParser) optionalBool(name string) *bool {
	raw := strings.TrimSpace(p.values.Get(name))
	if raw == "" {
		return nil
	}
	switch raw {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	}
	p.fail(name, "INVALID_BOOLEAN", nil)
	return nil
}

func (p *filterParser) boolean(name string) bool {
	v := p.optionalBool(name)
	return v != nil && *v
}

func (p *filterParser) date(name string) *time.Time {
	raw := strings.TrimSpace(p.values.Get(name))
	if raw == "" {
		return nil
	}
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		p.fail(name, "INVALID_DATE", map[string]any{"format": "YYYY-MM-DD"})
		return nil
	}
	return &t
}

func (p *filterParser) dateRange(fromName, toName string) (*time.Time, *time.Time) {
	from, to := p.date(fromName), p.date(toName)
	if from != nil && to != nil && from.After(*to) {
		p.fail(fromName, "AFTER_FIELD", map[string]any{"other": toName})
	}
	return from, to
}
