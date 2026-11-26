package taskcatalog

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestFlushEnvironmentCache(t *testing.T) {
	var got *http.Request
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.FlushEnvironmentCache(context.Background(), "production"); err != nil {
		t.Fatalf("FlushEnvironmentCache: %v", err)
	}
	if got == nil {
		t.Fatal("no request reached the server")
	}
	if got.Method != http.MethodDelete || got.URL.Path != "/puppet-admin-api/v1/environment-cache" {
		t.Errorf("request = %s %s", got.Method, got.URL.Path)
	}
	if env := got.URL.Query().Get("environment"); env != "production" {
		t.Errorf("environment = %q, want production", env)
	}
}

func TestFlushEnvironmentCacheErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		check  func(error) bool
	}{
		{http.StatusForbidden, func(err error) bool {
			var f *ForbiddenError
			return errors.As(err, &f) && strings.Contains(err.Error(), "DELETE")
		}},
		{http.StatusInternalServerError, func(err error) bool {
			var u *UnreachableError
			return errors.As(err, &u)
		}},
	} {
		c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		err := c.FlushEnvironmentCache(context.Background(), "production")
		if err == nil || !tc.check(err) {
			t.Errorf("status %d: err = %v", tc.status, err)
		}
	}
}

func TestFlushEnvironmentCacheNilClient(t *testing.T) {
	var c *Client
	if err := c.FlushEnvironmentCache(context.Background(), "production"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}
