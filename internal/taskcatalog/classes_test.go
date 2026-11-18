package taskcatalog

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// classesFixture is the response recorded from a real openvoxserver
// serving internal/classcatalog/testdata/modules/class_fixture (see
// task 1.2 in add-class-discovery-and-run-options).
func classesFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "classcatalog", "testdata", "api", "environment_classes_production.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestEnvironmentClasses(t *testing.T) {
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/puppet/v3/environment_classes" || r.URL.Query().Get("environment") != "production" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Header().Set("Etag", `"v1"`)
		w.Write(classesFixture(t))
	}))
	got, notModified, err := c.EnvironmentClasses(context.Background(), "production", "")
	if err != nil || notModified {
		t.Fatalf("EnvironmentClasses: %v (notModified %v)", err, notModified)
	}
	if got.ETag != `"v1"` {
		t.Errorf("etag = %q", got.ETag)
	}
	var fixture *ClassInfo
	var brokenErr string
	for i, f := range got.Files {
		for j, cl := range f.Classes {
			if cl.Name == "class_fixture" {
				fixture = &got.Files[i].Classes[j]
			}
		}
		if filepath.Base(f.Path) == "broken.pp" {
			brokenErr = f.Error
		}
	}
	if fixture == nil {
		t.Fatal("class_fixture not decoded")
	}
	if brokenErr == "" {
		t.Error("broken.pp's parse error not decoded")
	}
	params := map[string]ClassParam{}
	for _, p := range fixture.Params {
		params[p.Name] = p
	}
	if p := params["owner"]; p.HasDefault || p.Type != "String" {
		t.Errorf("owner = %+v, want a String with no default", p)
	}
	if p := params["port"]; !p.HasDefault || string(p.DefaultLiteral) != "8080" || p.Type != "Integer[1, 65535]" {
		t.Errorf("port = %+v", p)
	}
	if p := params["greeting"]; !p.HasDefault || p.DefaultLiteral != nil || p.DefaultSource == "" {
		t.Errorf("greeting = %+v, want an expression default with no literal", p)
	}
	if p := params["untyped"]; p.Type != "" || !p.HasDefault {
		t.Errorf("untyped = %+v", p)
	}
	if fixture.Params[0].Name != "owner" {
		t.Errorf("parameters not in declaration order: first is %q", fixture.Params[0].Name)
	}
}

func TestEnvironmentClasses_Conditional(t *testing.T) {
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write(classesFixture(t))
	}))
	got, notModified, err := c.EnvironmentClasses(context.Background(), "production", `"v1"`)
	if err != nil || !notModified || got != nil {
		t.Fatalf("got %v, notModified %v, err %v; want not modified", got, notModified, err)
	}
}

func TestEnvironmentClasses_Errors(t *testing.T) {
	status := http.StatusForbidden
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))

	_, _, err := c.EnvironmentClasses(context.Background(), "production", "")
	var fe *ForbiddenError
	if !errors.As(err, &fe) {
		t.Errorf("403: err = %v, want ForbiddenError", err)
	}
	var ue *UnreachableError
	if errors.As(err, &ue) {
		t.Error("403 must not read as unreachable")
	}

	status = http.StatusNotFound
	_, _, err = c.EnvironmentClasses(context.Background(), "nosuch", "")
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "environment" || nf.Name != "nosuch" {
		t.Errorf("404: err = %v, want environment not found", err)
	}

	status = http.StatusInternalServerError
	_, _, err = c.EnvironmentClasses(context.Background(), "production", "")
	if !errors.As(err, &ue) {
		t.Errorf("500: err = %v, want UnreachableError", err)
	}

	var none *Client
	if _, _, err := none.EnvironmentClasses(context.Background(), "production", ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil client: err = %v, want ErrNotConfigured", err)
	}
}

func TestCheckValue(t *testing.T) {
	cases := []struct {
		typ   string
		value any
		ok    bool
	}{
		{"Integer", float64(8080), true},
		{"Integer", "8080", false},
		{"Integer[1, 65535]", float64(70000), false},
		{"Boolean", true, true},
		{"Boolean", "true", false},
		{"Enum['absent', 'present']", "present", true},
		{"Enum['absent', 'present']", "gone", false},
		{"Array[String]", []any{"a", "b"}, true},
		{"Array[String]", []any{"a", float64(1)}, false},
		{"Optional[String]", nil, true},
		{"Optional[String]", float64(1), false},
		{"Variant[String, Integer]", float64(1), true},
		{"Variant[String, Integer]", true, false},
		{"Sensitive[String]", "secret", true},
		{"Sensitive[String]", float64(1), false},
		// Types this package does not evaluate accept anything.
		{"TypeReference['Stdlib::Absolutepath']", float64(1), true},
		{"Struct[{a => String}]", "x", true},
		{"", "anything", true},
		{"not a type", "x", true},
	}
	for _, tc := range cases {
		why := CheckValue(tc.typ, tc.value)
		if (why == "") != tc.ok {
			t.Errorf("CheckValue(%q, %v) = %q, want ok=%v", tc.typ, tc.value, why, tc.ok)
		}
	}
}
