package nodeconnectivity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/agentupgrade"
)

type fakeAgentBuilds struct {
	views map[string]agentupgrade.NodeView
	err   error
	auto  bool
}

func (f fakeAgentBuilds) Views(context.Context) (map[string]agentupgrade.NodeView, error) {
	return f.views, f.err
}

func (f fakeAgentBuilds) AutoUpgrade() bool { return f.auto }

// agentResponse decodes just what these tests look at.
type agentResponse struct {
	Nodes []struct {
		Certname  string `json:"certname"`
		Connected bool   `json:"connected"`
		Agent     *struct {
			Version    string `json:"version"`
			State      string `json:"state"`
			OutOfDate  bool   `json:"outOfDate"`
			CanUpgrade bool   `json:"canUpgrade"`
			Blocked    string `json:"blocked"`
			Upgrade    *struct {
				State    string `json:"state"`
				Error    string `json:"error"`
				Attempts int    `json:"attempts"`
			} `json:"upgrade"`
		} `json:"agent"`
	} `json:"nodes"`
	AgentAutoUpgrade *bool `json:"agentAutoUpgrade"`
}

func listWithAgents(t *testing.T, agents AgentBuilds) agentResponse {
	t.Helper()
	h := NewHandlers(&fakeRegistry{connected: map[string]bool{
		"current": true, "outdated": true, "old-client": true, "failed": true, "never-reported": true,
	}}, nil, nil, noopRecordAudit, noopRecordAuditRead, nil)
	if agents != nil {
		h.SetAgentBuilds(agents)
	}
	rec := httptest.NewRecorder()
	h.listConnected(rec, httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp agentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestListConnected_AgentBuilds(t *testing.T) {
	resp := listWithAgents(t, fakeAgentBuilds{auto: true, views: map[string]agentupgrade.NodeView{
		"current":    {Version: "v2", State: agentupgrade.StateCurrent},
		"outdated":   {Version: "v1", State: agentupgrade.StateOutdated, OutOfDate: true, CanUpgrade: true},
		"old-client": {State: agentupgrade.StateUnknown, OutOfDate: true, Blocked: agentupgrade.BlockedInstallScript},
		"failed": {Version: "v1", State: agentupgrade.StateOutdated, OutOfDate: true, CanUpgrade: true,
			Upgrade: &agentupgrade.UpgradeView{State: agentupgrade.UpgradeFailed, Error: "digest mismatch", Attempts: 2}},
	}})

	if resp.AgentAutoUpgrade == nil || !*resp.AgentAutoUpgrade {
		t.Error("agentAutoUpgrade is not reported as on")
	}
	if len(resp.Nodes) != 5 {
		t.Fatalf("%d nodes, want 5", len(resp.Nodes))
	}
	for _, n := range resp.Nodes {
		if n.Agent == nil {
			t.Fatalf("%s has no agent status", n.Certname)
		}
		a := n.Agent
		switch n.Certname {
		case "current":
			if a.State != "current" || a.OutOfDate || a.Version != "v2" || a.Upgrade != nil {
				t.Errorf("current: %+v", a)
			}
		case "outdated":
			if a.State != "outdated" || !a.OutOfDate || !a.CanUpgrade || a.Version != "v1" {
				t.Errorf("outdated: %+v", a)
			}
		case "old-client":
			if a.State != "unknown" || !a.OutOfDate || a.CanUpgrade || a.Blocked != "install-script" {
				t.Errorf("old-client: %+v", a)
			}
		case "failed":
			if !a.OutOfDate || a.Upgrade == nil || a.Upgrade.State != "failed" || a.Upgrade.Error != "digest mismatch" || a.Upgrade.Attempts != 2 {
				t.Errorf("failed: %+v / %+v", a, a.Upgrade)
			}
		case "never-reported":
			// Not yet reported is not the same as out of date.
			if a.State != "unreported" || a.OutOfDate || a.CanUpgrade {
				t.Errorf("never-reported: %+v", a)
			}
		}
	}
}

func TestListConnected_AutoUpgradeOffIsReported(t *testing.T) {
	resp := listWithAgents(t, fakeAgentBuilds{auto: false, views: map[string]agentupgrade.NodeView{}})
	if resp.AgentAutoUpgrade == nil || *resp.AgentAutoUpgrade {
		t.Error("agentAutoUpgrade is not reported as off")
	}
}

func TestListConnected_WithoutAgentBuilds(t *testing.T) {
	for name, agents := range map[string]AgentBuilds{
		"not configured": nil,
		"lookup fails":   fakeAgentBuilds{err: errors.New("database is down")},
	} {
		resp := listWithAgents(t, agents)
		if len(resp.Nodes) != 5 {
			t.Errorf("%s: %d nodes, want the connectivity list unaffected", name, len(resp.Nodes))
		}
		for _, n := range resp.Nodes {
			if n.Agent != nil || !n.Connected {
				t.Errorf("%s: node %s = %+v", name, n.Certname, n)
			}
		}
		if resp.AgentAutoUpgrade != nil {
			t.Errorf("%s: agentAutoUpgrade present", name)
		}
	}
}
