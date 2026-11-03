package pagination

type Response[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor,omitempty"`
	PrevCursor *string `json:"prev_cursor,omitempty"`
	HasNext    bool    `json:"has_next"`
	HasPrev    bool    `json:"has_prev"`
	TotalCount *int64  `json:"total_count,omitempty"`
}

func NewResponse[T any](items []T, hasNext, hasPrev bool, nextCursor, prevCursor *string) Response[T] {
	if items == nil {
		items = make([]T, 0)
	}
	if !hasNext {
		nextCursor = nil
	}
	if !hasPrev {
		prevCursor = nil
	}
	return Response[T]{
		Items:      items,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextCursor: nextCursor,
		PrevCursor: prevCursor,
		TotalCount: nil,
	}
}

func StringPtr[T any](value T) *T {
	return &value
}
