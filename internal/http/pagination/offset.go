package pagination

import (
	"fmt"
	"strconv"
	"strings"
)

type OffsetParams struct {
	Page   int
	Limit  int
	Offset int
}

func ParseOffset(rawPage, rawLimit string) (OffsetParams, error) {
	page := DefaultPage
	if trimmed := strings.TrimSpace(rawPage); trimmed != "" {
		parsedPage, err := strconv.Atoi(trimmed)
		if err != nil || parsedPage < 1 || parsedPage > MaxPage {
			return OffsetParams{}, fmt.Errorf("%w: page must be an integer between 1 and %d", ErrInvalidPage, MaxPage)
		}
		page = parsedPage
	}

	limit := DefaultLimit
	if trimmed := strings.TrimSpace(rawLimit); trimmed != "" {
		parsedLimit, err := strconv.Atoi(trimmed)
		if err != nil || parsedLimit < 1 || parsedLimit > MaxLimit {
			return OffsetParams{}, fmt.Errorf("%w: limit must be an integer between 1 and %d", ErrInvalidLimit, MaxLimit)
		}
		limit = parsedLimit
	}

	return OffsetParams{
		Page:   page,
		Limit:  limit,
		Offset: (page - 1) * limit,
	}, nil
}
