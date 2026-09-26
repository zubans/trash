package service

// pageLimit приводит limit клиента к границам страницы: неуказанный или
// неположительный — fallback (прежний потолок списка, чтобы существующие
// клиенты видели ту же первую страницу), больше max — max.
func pageLimit(limit, fallback, max int) int {
	switch {
	case limit <= 0:
		return fallback
	case limit > max:
		return max
	default:
		return limit
	}
}

// pageOffset отбрасывает отрицательное смещение.
func pageOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
