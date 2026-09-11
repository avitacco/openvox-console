// Package pagination provides the page/pageSize query-param parsing and
// response envelope shared by every paginated list endpoint (nodes,
// groups, activity, deploys), so paging behaves identically across the
// console's API. It has no dependency on any other internal package, so
// importing it doesn't create coupling between feature packages.
package pagination

import (
	"math"
	"net/http"
	"strconv"
)

// DefaultPageSize is used when a request omits pageSize.
const DefaultPageSize = 25

// MaxPageSize bounds pageSize, so a malformed or hostile request can't
// force an unbounded query.
const MaxPageSize = 100

// Page is one page of items of type T, plus enough metadata for a client
// to render pagination controls.
type Page[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

// New builds a Page from a store's result.
func New[T any](items []T, page, pageSize, total int) Page[T] {
	return Page[T]{Items: items, Page: page, PageSize: pageSize, Total: total}
}

// Params reads page/pageSize from r's query string, defaulting to page 1
// and DefaultPageSize, and clamping both to sane bounds.
func Params(r *http.Request) (page, pageSize int) {
	page = parseClamped(r.URL.Query().Get("page"), 1, math.MaxInt)
	pageSize = parseClamped(r.URL.Query().Get("pageSize"), DefaultPageSize, MaxPageSize)
	return page, pageSize
}

// Offset returns the SQL OFFSET for page/pageSize (page is 1-indexed).
func Offset(page, pageSize int) int {
	return (page - 1) * pageSize
}

func parseClamped(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
