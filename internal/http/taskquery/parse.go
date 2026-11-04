package taskquery

import (
	"fmt"
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

	sortKeys, err := parseSort(values.Get("sort"))
	if err != nil {
		return query, &httperrors.AppError{
			Code:    CodeInvalidSort,
			Message: "invalid sort",
			Status:  http.StatusBadRequest,
			Fields:  map[string]string{"sort": err.Error()},
			Err:     err,
		}
	}
	query.Sort = sortKeys

	p := &filterParser{values: values, fields: map[string]string{}}
	f := &query.Filter

	if q := strings.TrimSpace(values.Get("q")); q != "" {
		if utf8.RuneCountInString(q) > maxSearchLength {
			p.fail("q", fmt.Sprintf("must be at most %d characters", maxSearchLength))
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

	if len(p.fields) > 0 {
		return query, &httperrors.AppError{
			Code:    CodeInvalidFilter,
			Message: "invalid filter parameters",
			Status:  http.StatusBadRequest,
			Fields:  p.fields,
		}
	}
	return query, nil
}

func parseSort(raw string) ([]domain.TaskSortKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > domain.MaxTaskSortKeys {
		return nil, fmt.Errorf("at most %d fields are allowed", domain.MaxTaskSortKeys)
	}
	seen := map[domain.TaskSortField]struct{}{}
	keys := make([]domain.TaskSortKey, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		desc := strings.HasPrefix(part, "-")
		field := domain.TaskSortField(strings.TrimPrefix(part, "-"))
		if !domain.IsValidTaskSortField(field) {
			return nil, fmt.Errorf("unsupported sort field %q", part)
		}
		if _, dup := seen[field]; dup {
			return nil, fmt.Errorf("duplicate sort field %q", field)
		}
		seen[field] = struct{}{}
		keys = append(keys, domain.TaskSortKey{Field: field, Desc: desc})
	}
	return keys, nil
}

type filterParser struct {
	values url.Values
	fields map[string]string
}

func (p *filterParser) fail(name, msg string) {
	if _, exists := p.fields[name]; !exists {
		p.fields[name] = msg
	}
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
				p.fail(name, "empty list item")
				return nil, false
			}
			items = append(items, item)
		}
	}
	if len(items) > maxListValues {
		p.fail(name, fmt.Sprintf("at most %d values are allowed", maxListValues))
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
			p.fail(name, "must be a list of positive integers")
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
			p.fail(name, "must be a list of integers from 1 to 5")
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
	p.fail(name, "must be true or false")
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
		p.fail(name, "must be a date in YYYY-MM-DD format")
		return nil
	}
	return &t
}

func (p *filterParser) dateRange(fromName, toName string) (*time.Time, *time.Time) {
	from, to := p.date(fromName), p.date(toName)
	if from != nil && to != nil && from.After(*to) {
		p.fail(fromName, "must not be after "+toName)
	}
	return from, to
}
