package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// awaitJobEnd polls a job until it is no longer running and returns its
// body.
func awaitJobEnd(c sweepClient, token string, id int64) string {
	c.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	body := ""
	for time.Now().Before(deadline) {
		var status int
		status, body = c.get(fmt.Sprintf("/api/v1/orchestrator/jobs/%d", id), token)
		if status != http.StatusOK {
			c.t.Fatalf("read job %d = %d %s", id, status, body)
		}
		if !strings.Contains(body, `"status":"running"`) && !strings.Contains(body, `"status":"pending"`) {
			return body
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.t.Fatalf("job %d did not end: %s", id, body)
	return ""
}

// A job started through the API of a web instance is followed to its
// end by that instance: web serves the routes that start jobs, so it
// must also record what comes back. The node here is connected nowhere,
// which ends each job at once - before this was fixed the jobs stayed
// running until the reaper failed them hours later.
func TestWebInstanceFollowsTheJobsItStarts(t *testing.T) {
	cluster := testapp.NewCluster(t)
	web := cluster.Start(testapp.Config{Mode: runtime.ModeWeb})
	c := sweepClient{t: t, inst: web}
	admin := web.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)
	const node = "web-started.example.test"

	var run struct {
		ID int64 `json:"id"`
	}
	c.send(http.MethodPost, "/api/v1/orchestrator/runs", admin, map[string]any{"targets": []string{node}}, &run)
	if body := awaitJobEnd(c, admin, run.ID); !strings.Contains(body, `"status":"failed"`) || !strings.Contains(body, "agent not connected") {
		t.Errorf("run = %s; want it failed because the node is not connected", body)
	}

	var plan struct {
		ID int64 `json:"id"`
	}
	c.send(http.MethodPost, "/api/v1/orchestrator/plans", admin, map[string]any{
		"name": "from web", "targets": []string{node},
		"steps": []map[string]string{{"kind": "run"}, {"kind": "run"}},
	}, &plan)
	body := awaitJobEnd(c, admin, plan.ID)
	if !strings.Contains(body, `"status":"failed"`) || !strings.Contains(body, `"state":"not_run"`) {
		t.Errorf("plan = %s; want it failed at its first step with the second not run", body)
	}
}
