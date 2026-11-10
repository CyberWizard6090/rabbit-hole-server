package pagination

import (
	"encoding/json"
	"strconv"
)

const taskResource = "tasks"

type TaskCursorPosition struct {
	Keys []json.RawMessage `json:"k"`
}

func TaskCursorContext(listID uint, sort, filter string) CursorContext {
	scope := strconv.FormatUint(uint64(listID), 10)
	if filter != "" {
		scope += ":" + filter
	}
	return CursorContext{Resource: taskResource, Scope: scope, Sort: sort}
}
