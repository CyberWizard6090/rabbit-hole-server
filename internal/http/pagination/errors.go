package pagination

import "errors"

var (
	ErrInvalidCursor = errors.New("pagination: invalid cursor")
	ErrInvalidLimit  = errors.New("pagination: invalid limit")
	ErrInvalidPage   = errors.New("pagination: invalid page")
	ErrInvalidConfig = errors.New("pagination: invalid configuration")
)
