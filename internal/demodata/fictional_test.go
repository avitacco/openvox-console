package demodata

import (
	"net"
	"strings"
	"testing"
)

// These tests enforce the property that makes this data safe to publish:
// everything it names is fictional. A one-off review cannot hold that
// line - someone adds a node next year and reaches for a name from the
// infrastructure they happen to be thinking about - so it is a test.

// reservedSuffix is RFC 2606's documentation domain. Names under it
// cannot collide with a real host, which is the whole reason it exists.
const reservedSuffix = "." + Domain

func TestFleetNamesAreFictional(t *testing.T) {
	if Domain != "example.com" {
		t.Fatalf("Domain = %q, want the RFC 2606 reserved example.com", Domain)
	}

	for _, node := range Fleet {
		if !strings.HasSuffix(node.Certname, reservedSuffix) {
			t.Errorf("node %q is not under %s - a name outside the reserved domain can collide with a real host", node.Certname, Domain)
		}

		ip := net.ParseIP(node.IPAddress)
		if ip == nil {
			t.Errorf("node %q has unparseable address %q", node.Certname, node.IPAddress)
			continue
		}
		if !ip.IsPrivate() && !ip.IsLoopback() {
			t.Errorf("node %q has address %s, which is routable - demo data must use private space", node.Certname, ip)
		}
	}
}

func TestUsersAreFictional(t *testing.T) {
	for _, user := range Users {
		if !strings.HasSuffix(user.Email, "@"+Domain) {
			t.Errorf("user %q has email %q, which is not under %s", user.Username, user.Email, Domain)
		}
	}
}

func TestDemoPasswordIsObviouslyNotReal(t *testing.T) {
	// The seed refuses non-local targets, but a password that looked
	// plausible would still invite someone to reuse it.
	if !strings.Contains(DemoPassword, "not-a-real-password") {
		t.Errorf("DemoPassword = %q, which does not announce itself as fake", DemoPassword)
	}
}

func TestFleetCoversTheRequiredVariety(t *testing.T) {
	// The seed exists so fleet-wide views show a distribution. These
	// assertions are the floor below which the screenshots stop being
	// worth taking.
	counts := CountByStatus()
	for _, status := range []RunStatus{StatusUnchanged, StatusChanged, StatusFailed} {
		if counts[status] == 0 {
			t.Errorf("no node has run status %q - every outcome must be represented", status)
		}
	}

	families := map[string]bool{}
	for _, node := range Fleet {
		families[node.Platform.Family] = true
	}
	if len(families) < 3 {
		t.Errorf("fleet spans %d OS families, want at least 3", len(families))
	}

	if envs := Environments(); len(envs) < 2 {
		t.Errorf("fleet spans %d environments, want at least 2", len(envs))
	}
}

func TestEveryAdvisoryMatchesAnInstalledPackage(t *testing.T) {
	// A finding against a package nothing has installed produces a
	// vulnerability with no affected nodes - an empty row in exactly the
	// view the site screenshots.
	for _, advisory := range Advisories {
		var affected int
		for _, node := range Fleet {
			if _, has := InstalledVersion(node, advisory.Package); has {
				affected++
			}
		}
		if affected == 0 {
			t.Errorf("advisory %s names package %q, which no demo node has installed", advisory.VulnID, advisory.Package)
		}
	}
}

func TestEveryJobTargetsARealDemoNode(t *testing.T) {
	known := make(map[string]bool, len(Fleet))
	for _, n := range Fleet {
		known[n.Certname] = true
	}

	for _, job := range Jobs {
		if len(job.Targets) == 0 {
			t.Errorf("job %q has no targets", job.TaskName+job.PlanName)
		}
		for _, target := range job.Targets {
			if !known[target.Certname] {
				t.Errorf("job targets %q, which is not in the demo fleet", target.Certname)
			}
		}
	}
}

func TestGroupRulesMatchSomething(t *testing.T) {
	// Only the fact paths the demo fleet actually reports can match.
	// This catches a rule written against a fact the seed does not set,
	// which would render as a group with no members.
	reported := map[string]bool{
		"kernel": true, "os.family": true, "role": true,
		"puppet_environment": true, "osfamily": true,
		"operatingsystem": true, "architecture": true,
	}
	for _, group := range Groups {
		for _, cond := range group.Rule {
			if !reported[cond.FactPath] {
				t.Errorf("group %q matches on fact %q, which the demo fleet does not report", group.Name, cond.FactPath)
			}
		}
	}
}
