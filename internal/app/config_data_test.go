package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// A node's merged configuration data end to end: groups set it, the data
// route serves it to a token holding enc:read (the token the Puppet
// server's Hiera function uses), and a token without that permission is
// refused.
func TestConfigData_ServedToTheTokenTheHieraFunctionUses(t *testing.T) {
	cluster := testapp.NewCluster(t)
	backends := fakeOpenvoxdb(t, cluster)
	inst := cluster.Start(testapp.Config{Env: map[string]string{"CONSOLE_OPENVOXDB_URL": backends.URL}})
	c := sweepClient{t: t, inst: inst}
	admin := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	type idOnly struct {
		ID int64 `json:"id"`
	}
	var web, db idOnly
	c.send(http.MethodPost, "/api/v1/groups", admin, map[string]any{
		"name": "cfg-web", "priority": 108001,
		"rule":       []map[string]string{{"factPath": "role", "operator": "=", "value": "web"}},
		"configData": map[string]any{"ntp::servers": []string{"a.example"}, "dns::timeout": 5},
	}, &web)
	c.send(http.MethodPost, "/api/v1/groups", admin, map[string]any{
		"name": "cfg-db", "priority": 108002,
		"rule":       []map[string]string{{"factPath": "role", "operator": "=", "value": "db"}},
		"configData": map[string]any{"db::only": true},
	}, &db)

	var reader, noEnc struct {
		Token string `json:"token"`
	}
	c.send(http.MethodPost, "/api/v1/service-tokens", admin, map[string]any{"name": "cfg-enc", "permissions": []string{"enc:read"}}, &reader)
	c.send(http.MethodPost, "/api/v1/service-tokens", admin, map[string]any{"name": "cfg-other", "permissions": []string{"nodes:read"}}, &noEnc)

	status, body := c.get("/api/v1/enc/"+visibleNode+"/data", reader.Token)
	if status != http.StatusOK {
		t.Fatalf("data for the web node = %d %s", status, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["dns::timeout"] != 5.0 {
		t.Errorf("web node data = %v, want the web group's two keys and nothing from the db group", got)
	}

	status, body = c.get("/api/v1/enc/"+hiddenNode+"/data", reader.Token)
	var dbData map[string]any
	_ = json.Unmarshal([]byte(body), &dbData)
	if status != http.StatusOK || len(dbData) != 1 || dbData["db::only"] != true {
		t.Errorf("db node data = %d %v, want only db::only", status, dbData)
	}

	if status, _ := c.get("/api/v1/enc/"+visibleNode+"/data", noEnc.Token); status != http.StatusForbidden {
		t.Errorf("a token without enc:read = %d, want 403", status)
	}
	if status, _ := c.get("/api/v1/enc/"+visibleNode+"/data", ""); status != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", status)
	}
}
