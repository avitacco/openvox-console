package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

func mcp(t *testing.T, base, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func mcpCall(t *testing.T, base, token, tool, args string) (bool, string) {
	t.Helper()
	code, out := mcp(t, base, token, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args))
	if code != http.StatusOK || out["result"] == nil {
		t.Fatalf("tools/call %s = %d %v", tool, code, out)
	}
	result := out["result"].(map[string]any)
	return result["isError"] == true, result["content"].([]any)[0].(map[string]any)["text"].(string)
}

func TestAgentEndpoint_IsOffUnlessEnabled(t *testing.T) {
	cluster := testapp.NewCluster(t)
	off := cluster.Start(testapp.Config{})
	code, _ := mcp(t, off.BaseURL, "anything", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	// The path is not routed: it answers as any unknown path does, and never
	// as an authenticated endpoint would.
	if code == http.StatusOK || code == http.StatusUnauthorized {
		t.Errorf("POST /mcp with the endpoint off = %d, want the answer for an unknown path", code)
	}
}

// An agent asks, a person approves, and only then does a job exist.
func TestAgentEndpoint_AskApproveRun(t *testing.T) {
	cluster := testapp.NewCluster(t)
	inst := cluster.Start(testapp.Config{Env: map[string]string{"CONSOLE_MCP_ENABLED": "true"}})
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

	// The administrator issues the agent's token.
	var issued struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	c.send(http.MethodPost, "/api/v1/agent-tokens", admin, map[string]any{
		"name": "e2e-bot", "permissions": []string{"nodes:read", "orchestrator:read", "orchestrator:run"},
		"nodeGroups": []int64{root}, "expiresInDays": 7,
	}, &issued)
	agent := issued.Token

	// It is for the agent endpoint alone.
	if status, _ := c.get("/api/v1/orchestrator/jobs", agent); status != http.StatusUnauthorized {
		t.Errorf("an agent token on a REST endpoint = %d, want 401", status)
	}
	if code, _ := mcp(t, inst.BaseURL, admin, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != http.StatusUnauthorized {
		t.Errorf("a user token on /mcp = %d, want 401", code)
	}
	_, list := mcp(t, inst.BaseURL, agent, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !strings.Contains(fmt.Sprint(list), "request_puppet_run") {
		t.Fatalf("tools = %v", list)
	}

	// The API is described for anyone authenticated; an agent token is not
	// one that may read it (it may not call the REST API at all).
	var spec struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	c.send(http.MethodGet, "/api/v1/openapi.json", admin, nil, &spec)
	if spec.OpenAPI == "" || spec.Paths["/api/v1/orchestrator/tasks"] == nil {
		t.Errorf("openapi = %+v", spec)
	}
	if status, _ := c.get("/api/v1/openapi.json", agent); status != http.StatusUnauthorized {
		t.Errorf("openapi with an agent token = %d, want 401", status)
	}

	// Reads go through the console's own endpoints.
	if isErr, body := mcpCall(t, inst.BaseURL, agent, "list_jobs", `{}`); isErr {
		t.Errorf("list_jobs = %s", body)
	}

	// Asking stores a request and starts nothing.
	isErr, body := mcpCall(t, inst.BaseURL, agent, "request_puppet_run", `{"targets":["e2e-node"],"options":{"noop":true}}`)
	if isErr {
		t.Fatalf("request_puppet_run = %s", body)
	}
	var asked struct {
		RequestID int64  `json:"requestId"`
		Status    string `json:"status"`
	}
	_ = json.Unmarshal([]byte(body), &asked)
	if asked.Status != "pending" || asked.RequestID == 0 {
		t.Fatalf("answer = %s", body)
	}
	var jobs struct {
		Items []map[string]any `json:"items"`
	}
	c.send(http.MethodGet, "/api/v1/orchestrator/jobs?agent=true", admin, nil, &jobs)
	if len(jobs.Items) != 0 {
		t.Fatalf("an unapproved request started %d job(s)", len(jobs.Items))
	}

	// The person who issued the token may not approve its request.
	decide := fmt.Sprintf("/api/v1/agent-requests/%d/approve", asked.RequestID)
	if status, out := c.do(http.MethodPost, decide, admin, nil); status != http.StatusForbidden {
		t.Errorf("the token's owner approving = %d %s, want 403", status, out)
	}
	// Someone with no reach over the node may not either.
	var weak, weakUser struct {
		ID int64 `json:"id"`
	}
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{"name": "only-approves", "permissions": []string{"agent-requests:read", "agent-requests:approve"}, "nodeGroups": []int64{root}}, &weak)
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "weak-approver", "password": "weak-approver-password"}, &weakUser)
	c.send(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", weakUser.ID, weak.ID), admin, nil, nil)
	weakToken := inst.Login(t, "weak-approver", "weak-approver-password")
	if status, out := c.do(http.MethodPost, decide, weakToken, nil); status != http.StatusForbidden {
		t.Errorf("an approver who cannot run things there = %d %s, want 403", status, out)
	}

	// A proper approver can.
	var role, user struct {
		ID int64 `json:"id"`
	}
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "approver", "permissions": []string{"agent-requests:read", "agent-requests:approve", "orchestrator:run", "orchestrator:read"}, "nodeGroups": []int64{root},
	}, &role)
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "approver", "password": "approver-password-1"}, &user)
	c.send(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role.ID), admin, nil, nil)
	approver := inst.Login(t, "approver", "approver-password-1")

	var pending struct{ Count int }
	c.send(http.MethodGet, "/api/v1/agent-requests/pending-count", approver, nil, &pending)
	if pending.Count != 1 {
		t.Errorf("pending count = %d, want 1", pending.Count)
	}
	var approved struct {
		Status string `json:"status"`
		JobID  *int64 `json:"jobId"`
	}
	c.send(http.MethodPost, decide, approver, map[string]string{"note": "looks fine"}, &approved)
	if approved.Status != "approved" || approved.JobID == nil {
		t.Fatalf("approved = %+v", approved)
	}

	// The job records who asked and who approved.
	var job struct {
		TriggeredBy string `json:"triggeredBy"`
		Agent       struct {
			Token      string `json:"token"`
			ApprovedBy string `json:"approvedBy"`
		} `json:"agentRequest"`
	}
	c.send(http.MethodGet, fmt.Sprintf("/api/v1/orchestrator/jobs/%d", *approved.JobID), admin, nil, &job)
	if job.TriggeredBy != "agent:e2e-bot" || job.Agent.Token != "e2e-bot" || job.Agent.ApprovedBy != "approver" {
		t.Errorf("job = %+v", job)
	}
	// The agent sees the outcome.
	if _, got := mcpCall(t, inst.BaseURL, agent, "get_request", fmt.Sprintf(`{"requestId":%d}`, asked.RequestID)); !strings.Contains(got, "approved") || !strings.Contains(got, "jobId") {
		t.Errorf("get_request = %s", got)
	}

	// Revoking the token ends its access at once.
	c.send(http.MethodDelete, fmt.Sprintf("/api/v1/agent-tokens/%d", issued.ID), admin, nil, nil)
	if code, _ := mcp(t, inst.BaseURL, agent, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != http.StatusUnauthorized {
		t.Errorf("a revoked agent token = %d, want 401", code)
	}
}
