package domain

import (
	"context"
	"time"
)

type SearchType string

const (
	SearchTypeTask          SearchType = "task"
	SearchTypeComment       SearchType = "comment"
	SearchTypeChecklistItem SearchType = "checklist_item"
)

const (
	MinSearchQueryRunes = 2
	MaxSearchQueryRunes = 100
)

func AllSearchTypes() []SearchType {
	return []SearchType{SearchTypeTask, SearchTypeComment, SearchTypeChecklistItem}
}

type SearchParams struct {
	WorkspaceID uint
	Query       string
	Types       []SearchType
	SpaceID     *uint
	Limit       int
	Offset      int
}

type SearchHit struct {
	Type        SearchType
	ID          uint
	TaskID      uint
	ListID      uint
	SpaceID     uint
	TaskTitle   string
	Text        string
	Description *string
	Score       float64
	UpdatedAt   time.Time
}

type SearchMatch struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type SearchSnippet struct {
	Text    string        `json:"text"`
	Matches []SearchMatch `json:"matches"`
}

type SearchResult struct {
	Type      SearchType    `json:"type"`
	ID        uint          `json:"id"`
	TaskID    uint          `json:"task_id"`
	ListID    uint          `json:"list_id"`
	SpaceID   uint          `json:"space_id"`
	TaskTitle string        `json:"task_title"`
	Field     string        `json:"field"`
	Score     float64       `json:"score"`
	Snippet   SearchSnippet `json:"snippet"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type SearchRepository interface {
	Search(ctx context.Context, params SearchParams) ([]SearchHit, int64, error)
}
