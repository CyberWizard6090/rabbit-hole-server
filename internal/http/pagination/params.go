package pagination

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultPage  = 1
	DefaultLimit = 10
	MaxPage      = 10000
	MaxLimit     = 100
)

type Params struct {
	Limit  int
	Cursor *Cursor
}

func ParseLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxLimit {
		return 0, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidLimit, MaxLimit)
	}
	return limit, nil
}

func ParseParams(rawLimit, rawCursor string, codec *Codec, expected CursorContext) (Params, error) {
	limit, err := ParseLimit(rawLimit)
	if err != nil {
		return Params{}, err
	}
	cursor, err := codec.Decode(rawCursor)
	if err != nil {
		return Params{}, err
	}
	if cursor != nil {
		if err := cursor.ValidateContext(expected); err != nil {
			return Params{}, err
		}
	}
	return Params{Limit: limit, Cursor: cursor}, nil
}
