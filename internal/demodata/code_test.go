package demodata_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
)

// Every class a demo node group names has a manifest under code/, which
// the control repo fixture deploys (see the Makefile). The demo groups
// match real nodes too - any Linux node is in "Linux Servers" - so a
// class with no manifest fails those nodes' catalogs, and with them
// every Puppet run in the development stack.
func TestEveryDemoClassHasAManifest(t *testing.T) {
	for _, group := range demodata.Groups {
		for _, class := range group.Classes {
			module, name, ok := strings.Cut(class.Name, "::")
			if !ok {
				t.Errorf("group %q: class %q is not in a module", group.Name, class.Name)
				continue
			}
			path := filepath.Join("code", module, "manifests", name+".pp")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("group %q names %s, which has no manifest: %v", group.Name, class.Name, err)
				continue
			}
			if !strings.Contains(string(body), "class "+class.Name+" ") {
				t.Errorf("%s does not define %s", path, class.Name)
			}
			for param := range class.Parameters {
				if !strings.Contains(string(body), " $"+param+" ") {
					t.Errorf("group %q sets %s's %q, which %s does not declare", group.Name, class.Name, param, path)
				}
			}
		}
	}
}
