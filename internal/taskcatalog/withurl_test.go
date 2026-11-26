package taskcatalog

import (
	"context"
	"net/http"
	"testing"
)

func TestWithURLReusesCredential(t *testing.T) {
	var hit bool
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	other := c.WithURL(c.baseURL + "/")
	if other.baseURL != c.baseURL {
		t.Errorf("baseURL = %q, want %q", other.baseURL, c.baseURL)
	}
	if err := other.FlushEnvironmentCache(context.Background(), "production"); err != nil || !hit {
		t.Errorf("flush through WithURL: err %v, reached %v", err, hit)
	}
	var nilClient *Client
	if nilClient.WithURL("https://x:8140") != nil {
		t.Error("nil Client's WithURL is not nil")
	}
}
