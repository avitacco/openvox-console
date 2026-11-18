package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/scheduler"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// fastTicks makes instances started by this test look for due
// schedules every 200ms rather than every 20s.
func fastTicks(t *testing.T) {
	t.Helper()
	previous := scheduler.TickInterval
	scheduler.TickInterval = 200 * time.Millisecond
	t.Cleanup(func() { scheduler.TickInterval = previous })
}

type firingView struct {
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
	JobID   *int64 `json:"jobId"`
}

type scheduleView struct {
	ID        int64        `json:"id"`
	State     string       `json:"state"`
	Owner     string       `json:"owner"`
	NextRunAt *time.Time   `json:"nextRunAt"`
	Firings   []firingView `json:"firings"`
}

func getSchedule(c sweepClient, token string, id int64) scheduleView {
	c.t.Helper()
	var view scheduleView
	c.send(http.MethodGet, fmt.Sprintf("/api/v1/orchestrator/schedules/%d", id), token, nil, &view)
	return view
}

// awaitFiring polls until the schedule's first firing has an outcome.
func awaitFiring(c sweepClient, token string, id int64) firingView {
	c.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if view := getSchedule(c, token, id); len(view.Firings) > 0 && view.Firings[0].Outcome != scheduler.OutcomeFiring {
			return view.Firings[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.t.Fatalf("schedule %d did not fire", id)
	return firingView{}
}

func jobsOn(c sweepClient, token, certname string) (total int, body string) {
	c.t.Helper()
	var page struct {
		Total int `json:"total"`
	}
	status, body := c.get("/api/v1/orchestrator/jobs?target="+certname, token)
	if status != http.StatusOK {
		c.t.Fatalf("list jobs = %d %s", status, body)
	}
	_ = json.Unmarshal([]byte(body), &page)
	return page.Total, body
}

// Two instances both run the scheduler against one database. A schedule
// that comes due is started once, as a job naming the schedule and
// triggered by its owner - and a node that is not connected fails in
// that job as it would in an on-demand one.
func TestSchedule_FiresOnceAcrossInstances(t *testing.T) {
	fastTicks(t)
	cluster := testapp.NewCluster(t)
	instances := clusterOf(t, cluster, runtime.ModeAll, 2)
	c := sweepClient{t: t, inst: instances[0]}
	admin := instances[0].Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)
	const node = "scheduled-node.example.test"

	var created scheduleView
	c.send(http.MethodPost, "/api/v1/orchestrator/schedules", admin, map[string]any{
		"name": "soon", "kind": "run", "options": map[string]any{"noop": true},
		"targets": []string{node}, "startAt": time.Now().Add(1500 * time.Millisecond).UTC().Format(time.RFC3339Nano),
	}, &created)
	if created.State != scheduler.StateActive || created.Owner != testapp.BootstrapAdminUser {
		t.Fatalf("created = %+v, want active and owned by the administrator", created)
	}

	firing := awaitFiring(c, admin, created.ID)
	if firing.Outcome != scheduler.OutcomeStarted || firing.JobID == nil {
		t.Fatalf("firing = %+v, want a started job", firing)
	}
	// Give a second instance every chance to start it again.
	time.Sleep(time.Second)
	total, body := jobsOn(c, admin, node)
	if total != 1 {
		t.Fatalf("%d jobs were started for one due time, want 1: %s", total, body)
	}
	if !strings.Contains(body, `"schedule":{"id":`) || !strings.Contains(body, `"name":"soon"`) || !strings.Contains(body, `"noop":true`) {
		t.Errorf("job = %s; want it to name the schedule and carry its run options", body)
	}
	if view := getSchedule(c, admin, created.ID); view.State != scheduler.StateFinished || len(view.Firings) != 1 {
		t.Errorf("schedule after firing = %+v, want finished with one firing", view)
	}

	status, job := c.get(fmt.Sprintf("/api/v1/orchestrator/jobs/%d", *firing.JobID), admin)
	if status != http.StatusOK || !strings.Contains(job, `"triggeredBy":"`+testapp.BootstrapAdminUser+`"`) || !strings.Contains(job, "agent not connected") {
		t.Errorf("job = %d %s; want it triggered by the owner, with the unconnected node failed", status, job)
	}
}

// A schedule fires with its owner's permissions as they are then: a
// user whose role was removed after saving it starts nothing.
func TestSchedule_OwnerWhoLostThePermissionStartsNothing(t *testing.T) {
	fastTicks(t)
	cluster := testapp.NewCluster(t)
	inst := cluster.Start(testapp.Config{})
	c := sweepClient{t: t, inst: inst}
	admin := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	var groups struct {
		Items []struct {
			ID     int64 `json:"id"`
			IsRoot bool  `json:"isRoot"`
		} `json:"items"`
	}
	c.send(http.MethodGet, "/api/v1/groups", admin, nil, &groups)
	var root int64
	for _, g := range groups.Items {
		if g.IsRoot {
			root = g.ID
		}
	}
	if root == 0 {
		t.Fatal("no root group")
	}
	type idOnly struct {
		ID int64 `json:"id"`
	}
	var role, user idOnly
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "runners", "permissions": []string{"orchestrator:run", "orchestrator:read"}, "nodeGroups": []int64{root},
	}, &role)
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "runner", "password": "runner-password-1"}, &user)
	c.send(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role.ID), admin, nil, nil)
	runner := inst.Login(t, "runner", "runner-password-1")

	const node = "runner-node.example.test"
	var created scheduleView
	c.send(http.MethodPost, "/api/v1/orchestrator/schedules", runner, map[string]any{
		"name": "runner's", "kind": "run", "targets": []string{node},
		"startAt": time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano),
	}, &created)
	c.send(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role.ID), admin, nil, nil)

	firing := awaitFiring(c, admin, created.ID)
	if firing.Outcome != scheduler.OutcomeSkipped || !strings.Contains(firing.Detail, "no longer holds orchestrator:run") {
		t.Errorf("firing = %+v, want it skipped because the owner lost the permission", firing)
	}
	if total, body := jobsOn(c, admin, node); total != 0 {
		t.Errorf("a job was started: %s", body)
	}
	if status, body := c.get("/api/v1/audit-log?pageSize=50", admin); status != http.StatusOK || !strings.Contains(body, "schedule.skipped") {
		t.Errorf("activity log = %d, missing the skipped firing: %s", status, body)
	}
}

// Schedules are fired only where the scheduler runs. With only a web
// instance up, a due schedule waits; an orchestrator instance starting
// then picks it up, since it is not yet too late.
func TestSchedule_FiresOnlyWhereTheSchedulerRuns(t *testing.T) {
	fastTicks(t)
	cluster := testapp.NewCluster(t)
	web := cluster.Start(testapp.Config{Mode: runtime.ModeWeb})
	c := sweepClient{t: t, inst: web}
	admin := web.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	var created scheduleView
	c.send(http.MethodPost, "/api/v1/orchestrator/schedules", admin, map[string]any{
		"name": "waiting", "kind": "run", "targets": []string{"waiting-node.example.test"},
		"startAt": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano),
	}, &created)

	time.Sleep(2500 * time.Millisecond)
	if view := getSchedule(c, admin, created.ID); len(view.Firings) != 0 || view.NextRunAt == nil {
		t.Fatalf("with no scheduler running the schedule = %+v; want it still waiting", view)
	}

	cluster.Start(testapp.Config{Mode: runtime.ModeOrchestrator})
	if firing := awaitFiring(c, admin, created.ID); firing.Outcome != scheduler.OutcomeStarted {
		t.Errorf("firing = %+v, want it started once a scheduler was running", firing)
	}
}

// A scheduled plan is started once across instances, as a plan job with
// the steps the schedule was saved with - whatever has since happened
// to the saved plan it was made from.
func TestSchedule_PlanFiresOnceWithItsOwnSteps(t *testing.T) {
	fastTicks(t)
	cluster := testapp.NewCluster(t)
	instances := clusterOf(t, cluster, runtime.ModeAll, 2)
	c := sweepClient{t: t, inst: instances[0]}
	admin := instances[0].Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)
	const node = "planned-node.example.test"

	// A saved plan of two runs, and a schedule made from its steps.
	var saved struct {
		ID    int64             `json:"id"`
		Steps []json.RawMessage `json:"steps"`
	}
	c.send(http.MethodPost, "/api/v1/orchestrator/saved-plans", admin, map[string]any{
		"name":  "Two runs",
		"steps": []map[string]any{{"kind": "run", "options": map[string]any{"noop": true}}, {"kind": "run"}},
	}, &saved)
	var created scheduleView
	c.send(http.MethodPost, "/api/v1/orchestrator/schedules", admin, map[string]any{
		"name": "plan soon", "kind": "plan", "planName": "Two runs", "steps": saved.Steps,
		"targets": []string{node}, "startAt": time.Now().Add(1500 * time.Millisecond).UTC().Format(time.RFC3339Nano),
	}, &created)

	// The saved plan is changed and then deleted before the schedule
	// comes due.
	savedPath := fmt.Sprintf("/api/v1/orchestrator/saved-plans/%d", saved.ID)
	c.send(http.MethodPut, savedPath, admin, map[string]any{
		"name": "Two runs", "steps": []map[string]any{{"kind": "run"}, {"kind": "run"}, {"kind": "run"}},
	}, nil)
	if status, body := c.do(http.MethodDelete, savedPath, admin, nil); status != http.StatusNoContent {
		t.Fatalf("delete saved plan = %d %s", status, body)
	}

	firing := awaitFiring(c, admin, created.ID)
	if firing.Outcome != scheduler.OutcomeStarted || firing.JobID == nil {
		t.Fatalf("firing = %+v, want a started plan", firing)
	}
	time.Sleep(time.Second)
	var page struct {
		Total int `json:"total"`
	}
	c.send(http.MethodGet, "/api/v1/orchestrator/jobs?kind=plan", admin, nil, &page)
	if page.Total != 1 {
		t.Fatalf("%d plan jobs were started for one due time, want 1", page.Total)
	}

	var plan struct {
		Kind     string `json:"kind"`
		PlanName string `json:"planName"`
		Status   string `json:"status"`
		Steps    []struct {
			State      string `json:"state"`
			RunOptions *struct {
				Noop bool `json:"noop"`
			} `json:"runOptions"`
		} `json:"steps"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		c.send(http.MethodGet, fmt.Sprintf("/api/v1/orchestrator/jobs/%d", *firing.JobID), admin, nil, &plan)
		if plan.Status != "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Two steps, as saved - not the three the saved plan was changed
	// to. The node is not connected, so the first fails and the second
	// is closed without being sent.
	if plan.Kind != "plan" || plan.PlanName != "Two runs" || len(plan.Steps) != 2 {
		t.Fatalf("plan job = %+v, want the two steps the schedule was saved with", plan)
	}
	if plan.Steps[0].RunOptions == nil || !plan.Steps[0].RunOptions.Noop || plan.Steps[0].State != "failed" || plan.Steps[1].State != "not_run" || plan.Status != "failed" {
		t.Errorf("plan job = %+v, want a failed no-op step and a step that was not run", plan)
	}
}
