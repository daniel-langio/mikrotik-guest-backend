package main

import (
	"net/http"
	"strconv"
)

// The admin history panels show 10 rows at a time, newest first.
const pageSize = 10

// Pager is the prev/next state of one history panel; Base is its URL.
type Pager struct {
	Base             string
	Page             int
	HasPrev, HasNext bool
	Prev, Next       int
}

func pageParam(r *http.Request) int {
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 1 {
		return n
	}
	return 1
}

// pageRows trims the pageSize+1 rows a query fetched down to one page; the
// extra row only tells us whether a next page exists.
func pageRows[T any](rows []T, base string, page int) (map[string]any, Pager) {
	more := len(rows) > pageSize
	if more {
		rows = rows[:pageSize]
	}
	p := Pager{Base: base, Page: page, HasPrev: page > 1, HasNext: more, Prev: page - 1, Next: page + 1}
	return map[string]any{"Rows": rows, "Pager": p}, p
}
