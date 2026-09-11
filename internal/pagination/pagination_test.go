package pagination

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParams_Defaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	page, pageSize := Params(req)
	if page != 1 {
		t.Errorf("page = %d, want 1", page)
	}
	if pageSize != DefaultPageSize {
		t.Errorf("pageSize = %d, want %d", pageSize, DefaultPageSize)
	}
}

func TestParams_ReadsValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes?page=3&pageSize=10", nil)
	page, pageSize := Params(req)
	if page != 3 {
		t.Errorf("page = %d, want 3", page)
	}
	if pageSize != 10 {
		t.Errorf("pageSize = %d, want 10", pageSize)
	}
}

func TestParams_ClampsPageSizeToMax(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes?pageSize=99999", nil)
	_, pageSize := Params(req)
	if pageSize != MaxPageSize {
		t.Errorf("pageSize = %d, want clamped to %d", pageSize, MaxPageSize)
	}
}

func TestParams_InvalidOrZeroFallsBackToDefault(t *testing.T) {
	for _, qs := range []string{"page=0", "page=-1", "page=nope", "pageSize=0", "pageSize=-5", "pageSize=abc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes?"+qs, nil)
		page, pageSize := Params(req)
		if page < 1 || pageSize < 1 {
			t.Errorf("query %q: page=%d pageSize=%d, want both >= 1", qs, page, pageSize)
		}
	}
}

func TestOffset(t *testing.T) {
	cases := []struct{ page, pageSize, want int }{
		{1, 25, 0},
		{2, 25, 25},
		{3, 10, 20},
	}
	for _, c := range cases {
		if got := Offset(c.page, c.pageSize); got != c.want {
			t.Errorf("Offset(%d, %d) = %d, want %d", c.page, c.pageSize, got, c.want)
		}
	}
}
