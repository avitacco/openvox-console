package app

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/classcatalog"
	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/codemanager"
	"github.com/voxpupuli/enterprise-console/internal/encapi"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// classSource is openvoxserver's class listing in one of three states.
type classSource struct {
	err   error
	reads int
}

func (s *classSource) Environments(context.Context) ([]string, error) {
	return []string{"production"}, s.err
}

func (s *classSource) EnvironmentClasses(context.Context, string, string) (*taskcatalog.EnvironmentClasses, bool, error) {
	s.reads++
	if s.err != nil {
		return nil, false, s.err
	}
	return &taskcatalog.EnvironmentClasses{Name: "production", Files: []taskcatalog.ClassFile{{
		Path:    "/etc/puppetlabs/code/environments/production/manifests/site.pp",
		Classes: []taskcatalog.ClassInfo{{Name: "ntp", Params: []taskcatalog.ClassParam{{Name: "servers", Type: "Array[String]", HasDefault: true}}}},
	}}}, false, nil
}

func TestCheckGroupClasses(t *testing.T) {
	classes := []classifier.Class{
		{Name: "ntp", Parameters: map[string]any{"servers": "not-an-array", "colour": "red"}},
		{Name: "gone"},
	}

	warnings, checked := checkGroupClasses(classcatalog.New(&classSource{}))(context.Background(), "production", classes)
	if !checked || len(warnings) != 3 {
		t.Fatalf("checked %v, warnings %+v", checked, warnings)
	}
	kinds := []string{warnings[0].Kind, warnings[1].Kind, warnings[2].Kind}
	if strings.Join(kinds, ",") != "parameter-unknown,parameter-type,class-missing" {
		t.Errorf("kinds = %v", kinds)
	}

	for _, err := range []error{
		taskcatalog.ErrNotConfigured,
		&taskcatalog.ForbiddenError{Path: "/puppet/v3/environment_classes"},
		&taskcatalog.UnreachableError{Err: errors.New("down")},
	} {
		warnings, checked := checkGroupClasses(classcatalog.New(&classSource{err: err}))(context.Background(), "production", classes)
		if checked || len(warnings) != 0 {
			t.Errorf("%v: checked %v warnings %+v; unavailable discovery must flag nothing", err, checked, warnings)
		}
	}

	// A nil task catalogue client - no server connection configured - is
	// a valid source.
	var none *taskcatalog.Client
	if _, checked := checkGroupClasses(classcatalog.New(none))(context.Background(), "production", classes); checked {
		t.Error("an unconfigured catalogue reported the classes as checked")
	}
}

type encGroups struct{ groups []classifier.Group }

func (g encGroups) ListAllGroups(context.Context) ([]classifier.Group, error) { return g.groups, nil }

type encFacts struct{}

func (encFacts) Facts(context.Context, string) ([]openvoxdb.Fact, error) {
	return []openvoxdb.Fact{{Certname: "web01", Name: "kernel", Value: json.RawMessage(`"Linux"`)}}, nil
}

// What a node is classified with is the same whether discovery works,
// is unavailable, or says a class does not exist.
func TestENCAnswer_UnaffectedByClassDiscovery(t *testing.T) {
	groups := encGroups{groups: []classifier.Group{{
		ID: 2, Name: "linux", Priority: 1,
		Rule:    []classifier.Condition{{FactPath: "kernel", Operator: "=", Value: "Linux"}},
		Classes: []classifier.Class{{Name: "ntp", Parameters: map[string]any{"servers": []any{"a"}}}, {Name: "gone", Parameters: map[string]any{"x": 1}}},
	}}}
	open := func(_ string, next http.HandlerFunc) http.HandlerFunc { return next }

	answer := func() string {
		mux := http.NewServeMux()
		encapi.NewHandlers(groups, encFacts{}).Register(mux, open)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/enc/web01", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("enc: %d %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}

	states := map[string]*classSource{
		"available, reporting a class missing": {},
		"not configured":                       {err: taskcatalog.ErrNotConfigured},
		"unreachable":                          {err: &taskcatalog.UnreachableError{Err: errors.New("down")}},
	}
	var first string
	for name, src := range states {
		// Discovery is live in this state - a group read does consult
		// it - and the ENC answer is taken alongside.
		catalog := classcatalog.New(src)
		checkGroupClasses(catalog)(context.Background(), "production", groups.groups[0].Classes)
		if src.reads == 0 {
			t.Fatalf("%s: discovery was not consulted by the group check", name)
		}
		before := src.reads
		got := answer()
		if src.reads != before {
			t.Errorf("%s: classifying a node read the class catalogue", name)
		}
		if !strings.Contains(got, "gone") || !strings.Contains(got, "ntp") {
			t.Errorf("%s: ENC answer lost a class: %s", name, got)
		}
		if first == "" {
			first = got
		} else if got != first {
			t.Errorf("%s: ENC answer differs:\n%s\nvs\n%s", name, got, first)
		}
	}
}

// The guarantee above holds by construction as long as nothing on the
// classification path can reach the class catalogue at all.
func TestClassificationPathDoesNotImportClassDiscovery(t *testing.T) {
	for _, pkg := range []string{"classifier", "encapi"} {
		dir := filepath.Join("..", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				if strings.Contains(imp.Path.Value, "/classcatalog") || strings.Contains(imp.Path.Value, "/taskcatalog") {
					t.Errorf("internal/%s/%s imports %s", pkg, e.Name(), imp.Path.Value)
				}
			}
		}
	}
}

// A completed deploy makes every environment's classes be read again.
func TestDeployExpiresClassCatalog(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	src := &classSource{}
	catalog := classcatalog.New(src)
	expired := make(chan struct{}, 1)
	if _, err := codemanager.SubscribeDeployed(bus, func(codemanager.DeployedEvent) {
		catalog.ExpireAll()
		expired <- struct{}{}
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	catalog.Classes(ctx, "production")
	catalog.Classes(ctx, "production")
	if src.reads != 1 {
		t.Fatalf("reads = %d before the deploy, want 1", src.reads)
	}
	if err := codemanager.PublishDeployed(bus, "control", "production", "abc123"); err != nil {
		t.Fatal(err)
	}
	<-expired
	catalog.Classes(ctx, "production")
	if src.reads != 2 {
		t.Fatalf("reads = %d after the deploy, want 2", src.reads)
	}
}
