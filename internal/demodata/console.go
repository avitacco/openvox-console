package demodata

// This file holds the console-side demo records: node groups, people and
// roles. As with the fleet, every name is fictional - people are named
// after the roles they play, at the same reserved example.com domain.

// Group is one node group the seed creates, with the match rule that
// decides its membership.
type Group struct {
	Name        string
	Environment string
	Priority    int
	Classes     []GroupClass
	Parameters  map[string]any
	Rule        []GroupCondition
}

// GroupClass is a Puppet class a group applies.
type GroupClass struct {
	Name       string
	Parameters map[string]any
}

// GroupCondition is one fact predicate. A group's rule is the AND of all
// of them.
type GroupCondition struct {
	FactPath string
	Operator string
	Value    string
}

// Groups are the demo node groups. Each rule is written against facts the
// demo fleet actually reports, so every group has real members - a group
// matching nothing is an empty table in a screenshot, which is exactly
// what the site is trying to avoid showing.
//
// Priorities are unique and explicit because this classifier decides
// precedence entirely by priority, with no group hierarchy.
var Groups = []Group{
	{
		Name:        "All Nodes",
		Environment: "production",
		Priority:    10,
		Classes:     []GroupClass{{Name: "profile::base", Parameters: map[string]any{"manage_ntp": true}}},
		Parameters:  map[string]any{"monitoring_enabled": true},
		Rule:        []GroupCondition{{FactPath: "kernel", Operator: "~", Value: "."}},
	},
	{
		Name:        "Linux Servers",
		Environment: "production",
		Priority:    20,
		Classes:     []GroupClass{{Name: "profile::linux", Parameters: map[string]any{}}},
		Parameters:  map[string]any{},
		Rule:        []GroupCondition{{FactPath: "kernel", Operator: "=", Value: "Linux"}},
	},
	{
		Name:        "Windows Servers",
		Environment: "production",
		Priority:    30,
		Classes:     []GroupClass{{Name: "profile::windows", Parameters: map[string]any{}}},
		Parameters:  map[string]any{"patch_window": "sunday"},
		Rule:        []GroupCondition{{FactPath: "kernel", Operator: "=", Value: "windows"}},
	},
	{
		Name:        "Debian Family",
		Environment: "production",
		Priority:    40,
		Classes:     []GroupClass{{Name: "profile::apt", Parameters: map[string]any{"purge_sources": false}}},
		Parameters:  map[string]any{},
		Rule:        []GroupCondition{{FactPath: "os.family", Operator: "=", Value: "Debian"}},
	},
	{
		Name:        "RedHat Family",
		Environment: "production",
		Priority:    50,
		Classes:     []GroupClass{{Name: "profile::dnf", Parameters: map[string]any{}}},
		Parameters:  map[string]any{},
		Rule:        []GroupCondition{{FactPath: "os.family", Operator: "=", Value: "RedHat"}},
	},
	{
		Name:        "Web Tier",
		Environment: "production",
		Priority:    60,
		Classes: []GroupClass{
			{Name: "profile::nginx", Parameters: map[string]any{"worker_processes": 4}},
			{Name: "profile::tls", Parameters: map[string]any{}},
		},
		Parameters: map[string]any{"vhost_root": "/srv/www"},
		Rule:       []GroupCondition{{FactPath: "role", Operator: "=", Value: "web"}},
	},
	{
		Name:        "Database Tier",
		Environment: "production",
		Priority:    70,
		Classes:     []GroupClass{{Name: "profile::postgresql", Parameters: map[string]any{"max_connections": 400}}},
		Parameters:  map[string]any{"backup_schedule": "0 2 * * *"},
		Rule:        []GroupCondition{{FactPath: "role", Operator: "=", Value: "database"}},
	},
	{
		Name:        "Staging",
		Environment: "staging",
		Priority:    80,
		Classes:     []GroupClass{{Name: "profile::staging", Parameters: map[string]any{}}},
		Parameters:  map[string]any{"debug_logging": true},
		Rule:        []GroupCondition{{FactPath: "puppet_environment", Operator: "=", Value: "staging"}},
	},
	{
		Name:        "Team A Services",
		Environment: "team_a_production",
		Priority:    90,
		Classes:     []GroupClass{{Name: "profile::api", Parameters: map[string]any{}}},
		Parameters:  map[string]any{"owner": "team-a"},
		Rule:        []GroupCondition{{FactPath: "puppet_environment", Operator: "=", Value: "team_a_production"}},
	},
}

// Role is a demo RBAC role.
type Role struct {
	Name        string
	Permissions []string
}

// Roles are the demo roles, spanning read-only through operator. They
// exist so the Roles page shows a realistic permission spread rather than
// the single built-in admin role.
var Roles = []Role{
	{Name: "Auditor", Permissions: []string{"nodes:read", "classifier:read", "activity:read", "code:read", "orchestrator:read", "vulnerabilities:read"}},
	{Name: "Operator", Permissions: []string{"nodes:read", "nodes:manage", "classifier:read", "orchestrator:read", "orchestrator:run", "code:read", "vulnerabilities:read"}},
	{Name: "Classifier Editor", Permissions: []string{"nodes:read", "classifier:read", "classifier:write"}},
	{Name: "Release Engineer", Permissions: []string{"nodes:read", "code:read", "code:deploy", "orchestrator:read", "orchestrator:run"}},
	{Name: "Security Analyst", Permissions: []string{"nodes:read", "vulnerabilities:read", "vulnerabilities:manage", "activity:read"}},
}

// User is a demo console user.
//
// Passwords are a fixed throwaway string: these accounts exist only on a
// local demo stack that the seed refuses to run against anything else,
// and a generated password would break determinism for no benefit, since
// no screenshot shows one.
type User struct {
	Username  string
	FirstName string
	LastName  string
	Email     string
	Roles     []string
}

// DemoPassword is the password every seeded user gets. It is deliberately
// obvious: nothing here should ever be mistaken for a real credential,
// and the seed will not run against a non-local console.
const DemoPassword = "demo-only-not-a-real-password"

// Users are the demo people. Names are plainly fictional and describe the
// role the account plays rather than imitating real individuals.
var Users = []User{
	{Username: "a.auditor", FirstName: "Avery", LastName: "Auditor", Email: "a.auditor@" + Domain, Roles: []string{"Auditor"}},
	{Username: "o.operator", FirstName: "Oleg", LastName: "Operator", Email: "o.operator@" + Domain, Roles: []string{"Operator"}},
	{Username: "c.editor", FirstName: "Chris", LastName: "Editor", Email: "c.editor@" + Domain, Roles: []string{"Classifier Editor"}},
	{Username: "r.engineer", FirstName: "Robin", LastName: "Engineer", Email: "r.engineer@" + Domain, Roles: []string{"Release Engineer"}},
	{Username: "s.analyst", FirstName: "Sam", LastName: "Analyst", Email: "s.analyst@" + Domain, Roles: []string{"Security Analyst"}},
	{Username: "j.newstarter", FirstName: "Jules", LastName: "Newstarter", Email: "j.newstarter@" + Domain, Roles: []string{"Auditor", "Operator"}},
}

// ServiceToken is a demo machine credential.
type ServiceToken struct {
	Name        string
	Permissions []string
}

// ServiceTokens are the demo service tokens - the non-human credentials
// an integration would hold.
var ServiceTokens = []ServiceToken{
	{Name: "ci-deploy-pipeline", Permissions: []string{"code:read", "code:deploy"}},
	{Name: "monitoring-scraper", Permissions: []string{"nodes:read", "orchestrator:read"}},
	{Name: "enc-bridge", Permissions: []string{"enc:read"}},
}
