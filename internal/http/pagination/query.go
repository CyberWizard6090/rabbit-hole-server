package pagination

func FetchLimit(limit int) (int, error) {
	if limit < 1 || limit > MaxLimit {
		return 0, ErrInvalidLimit
	}
	return limit + 1, nil
}

func TrimExtra[T any](rows []T, limit int) ([]T, bool, error) {
	if limit < 1 || limit > MaxLimit {
		return nil, false, ErrInvalidLimit
	}
	if len(rows) > limit {
		return rows[:limit], true, nil
	}
	return rows, false, nil
}
