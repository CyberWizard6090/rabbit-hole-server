package pagination

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	DefaultPage  = 1
	DefaultLimit = 10
	MaxLimit     = 100
	MaxPage      = 1_000_000
)

type Params struct {
	Page   int
	Limit  int
	Offset int
	//	TODO: Добавить поддержку Cursor-based пагинации
}

func Parse(c *gin.Context) Params {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = DefaultPage
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	switch {
	case err != nil || limit < 1:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}
	if page > MaxPage {
		page = MaxPage
	}

	offset := (page - 1) * limit

	return Params{
		Page:   page,
		Limit:  limit,
		Offset: offset,
	}
}

func Pages(total int64, limit int) int {
	if limit < 1 || total < 1 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}
