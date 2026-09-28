package autopilot

import (
	"net/url"
	"strconv"
)

// pageParams — разобранные limit/offset листингов runs/deliveries (contract:
// default limit=20, max 100; default offset=0).
type pageParams struct {
	limit  int
	offset int
}

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// queryPage читает limit/offset из query-строки листинга. Принимает
// url.Values (а не *http.Request целиком), чтобы не зависеть от того, откуда
// взяты параметры — сам запрос или, в тесте, вручную собранный url.Values.
func queryPage(q url.Values) pageParams {
	return pageParams{
		limit:  clampLimit(positiveIntOrDefault(q.Get("limit"), defaultPageLimit)),
		offset: nonNegativeIntOrDefault(q.Get("offset"), 0),
	}
}

func clampLimit(n int) int {
	if n > maxPageLimit {
		return maxPageLimit
	}
	return n
}

func positiveIntOrDefault(raw string, def int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func nonNegativeIntOrDefault(raw string, def int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}
