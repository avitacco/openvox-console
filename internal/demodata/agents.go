package demodata

import "time"

// AgentToken is a demo credential issued to an AI agent. Its signed token
// does not exist: only the registry row the Agent tokens page lists is
// written, so nothing here can be used to sign in anywhere.
type AgentToken struct {
	Name        string
	Permissions []string
	// NodeGroups are the groups the token reaches, by name.
	NodeGroups []string
	// Days is how long until it expires, counted from when the demo is
	// seeded: a credential that has already expired would show an Expired
	// badge instead of the page's normal state.
	Days int
}

// AgentTokens are the demo agent tokens.
var AgentTokens = []AgentToken{
	{
		Name:        "ops-assistant",
		Permissions: []string{"nodes:read", "classifier:read", "orchestrator:read", "orchestrator:run"},
		NodeGroups:  []string{"Linux Servers"},
		Days:        30,
	},
	{
		Name:        "incident-summarizer",
		Permissions: []string{"nodes:read", "orchestrator:read", "vulnerabilities:read"},
		NodeGroups:  []string{"All Nodes"},
		Days:        14,
	},
}

// AgentRequest is a change a demo agent has asked for.
//
// Requests are dated from when the demo is seeded, not from the demo
// Instant. A pending request expires, in the real clock, so one dated to a
// moment months ago would read as expired the moment it was written - and
// the page that shows what a person approves would have nothing to approve.
type AgentRequest struct {
	Token   string
	Kind    string // task, run, plan, patch
	Summary string
	// Body is the request as stored, as JSON.
	Body    string
	Targets []string
	// Status is pending, rejected or expired.
	Status string
	// Age is how long before the seed the agent asked.
	Age  time.Duration
	Note string
	// DecidedBy is the user who rejected it.
	DecidedBy string
}

func nodes(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + "." + Domain
	}
	return out
}

// AgentRequests are the demo requests: three waiting for a decision, one
// rejected and one that nobody decided in time. They are written in this
// order, so the last - the nginx restart - is the newest, and the one the
// Approvals page lists first and the screenshot opens.
var AgentRequests = []AgentRequest{
	{
		Token:   "ops-assistant",
		Kind:    "patch",
		Summary: "a patch run on 5 nodes",
		Body:    `{"target":{"nodes":["web-01.prod.example.com","web-02.prod.example.com","web-03.prod.example.com","web-04.prod.example.com","web-05.prod.example.com"]},"options":{"scope":"security","batchSize":2,"stopAfterFailures":1,"reboot":false}}`,
		Targets: nodes("web-01.prod", "web-02.prod", "web-03.prod", "web-04.prod", "web-05.prod"),
		Status:  "pending",
		Age:     19 * time.Minute,
	},
	{
		Token:   "ops-assistant",
		Kind:    "run",
		Summary: "a Puppet run on 3 nodes",
		Body:    `{"target":{"nodes":["db-01.prod.example.com","db-02.prod.example.com","db-03.prod.example.com"]},"options":{"noop":true}}`,
		Targets: nodes("db-01.prod", "db-02.prod", "db-03.prod"),
		Status:  "pending",
		Age:     31 * time.Minute,
	},
	{
		Token:     "ops-assistant",
		Kind:      "task",
		Summary:   "task package::install on lb-01.prod.example.com",
		Body:      `{"target":{"nodes":["lb-01.prod.example.com"]},"task":"package::install","environment":"production","params":{"name":"haproxy","version":"2.9.7"}}`,
		Targets:   nodes("lb-01.prod"),
		Status:    "rejected",
		Age:       3 * time.Hour,
		Note:      "Not during the change freeze; ask again after Thursday.",
		DecidedBy: "o.operator",
	},
	{
		Token:   "ops-assistant",
		Kind:    "run",
		Summary: "a Puppet run on web-05.prod.example.com",
		Body:    `{"target":{"nodes":["web-05.prod.example.com"]}}`,
		Targets: nodes("web-05.prod"),
		Status:  "expired",
		Age:     26 * time.Hour,
	},
	{
		Token:   "ops-assistant",
		Kind:    "task",
		Summary: "task service::restart on 3 nodes",
		Body:    `{"target":{"nodes":["web-01.prod.example.com","web-02.prod.example.com","web-03.prod.example.com"]},"task":"service::restart","environment":"production","params":{"name":"nginx"}}`,
		Targets: nodes("web-01.prod", "web-02.prod", "web-03.prod"),
		Status:  "pending",
		Age:     6 * time.Minute,
	},
}
