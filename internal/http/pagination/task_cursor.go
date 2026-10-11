package pagination

import (
	"strconv"
	"time"
)

const (
	taskResource = "tasks"
	taskSort     = "created_at_desc_id_desc"
)

type TaskCursorPosition struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uint64    `json:"id"`
}

func TaskCursorContext(listID uint) CursorContext {
	return CursorContext{
		Resource: taskResource,
		Scope:    strconv.FormatUint(uint64(listID), 10),
		Sort:     taskSort,
	}
}
