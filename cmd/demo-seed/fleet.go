package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// producer is the certname openvoxdb records as having submitted these
// commands. It is the seed itself rather than a Puppet server, because
// pretending otherwise would put a fictional server name into a field
// operators use to trace where data came from.
const producer = "demo-seed.invalid"

// seedFleet writes the demo fleet into openvoxdb over its command API,
// the same interface a real Puppet server uses. Each node gets facts (so
// it appears in inventory, with a package inventory), a catalog, and a
// report (so it has a run outcome and a report timestamp).
//
// Commands are submitted per node rather than in one batch because
// openvoxdb's command API is per-certname; the ordering within a node
// matters (facts before report, so the node exists when the report
// lands), the ordering between nodes does not.
func seedFleet(ctx context.Context, opts options) error {
	client, err := openvoxdb.NewClient(opts.openvoxdbURL, opts.openvoxdbCert, opts.openvoxdbKey, opts.openvoxdbCA)
	if err != nil {
		return err
	}

	for _, node := range demodata.Fleet {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := submitFacts(ctx, client, node); err != nil {
			return fmt.Errorf("facts for %s: %w", node.Certname, err)
		}
		if err := submitCatalog(ctx, client, node); err != nil {
			return fmt.Errorf("catalog for %s: %w", node.Certname, err)
		}
		if err := submitReport(ctx, client, node); err != nil {
			return fmt.Errorf("report for %s: %w", node.Certname, err)
		}
	}

	fmt.Printf("    %d nodes across %d environments\n", len(demodata.Fleet), len(demodata.Environments()))

	if err := verifyFleetLanded(ctx, client); err != nil {
		return err
	}
	return warnAboutForeignNodes(ctx, client)
}

// verifyFleetLanded checks that what the seed submitted is actually
// queryable, and fails loudly when it is not.
//
// This exists because openvoxdb's command API acknowledges a command by
// putting it on a queue: a 200 means "accepted", not "stored". A command
// it later rejects, or data it silently declines to keep, shows up
// nowhere except openvoxdb's own log. Without this check the first sign
// of trouble is a published screenshot of an empty page.
//
// The specific trap it was written for: openvoxdb drops the resource
// events of a report dated outside its retention window, keeping the
// report row itself. Statuses look right, and every report detail page
// is empty.
func verifyFleetLanded(ctx context.Context, client *openvoxdb.Client) error {
	// openvoxdb processes commands asynchronously, so poll rather than
	// assuming the queue has drained.
	var nodes []openvoxdb.Node
	deadline := time.Now().Add(60 * time.Second)
	for {
		var err error
		nodes, err = client.Nodes(ctx)
		if err != nil {
			return fmt.Errorf("verifying the fleet landed: %w", err)
		}
		if countSeeded(nodes) == len(demodata.Fleet) || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	if got, want := countSeeded(nodes), len(demodata.Fleet); got != want {
		return fmt.Errorf("only %d of %d seeded nodes reached openvoxdb after 60s - check openvoxdb's log for rejected commands", got, want)
	}

	// Every node whose run changed something must have resource events,
	// or its report page screenshots empty.
	for _, node := range demodata.Fleet {
		if node.Status == demodata.StatusUnchanged {
			continue
		}
		events, err := eventCount(ctx, client, node.Certname)
		if err != nil {
			return fmt.Errorf("verifying events for %s: %w", node.Certname, err)
		}
		if events > 0 {
			continue
		}
		return fmt.Errorf(`%s reported %s but openvoxdb kept none of its resource events.

Two things cause this:

  1. openvoxdb's retention is dropping them. The demo fleet is dated
     %s, which the stock 14d/7d settings treat as already expired.
     Capture must run with docker-compose.screenshots.yml layered on:
       docker compose -f docker-compose.yml -f docker-compose.override.yml \
                      -f docker-compose.screenshots.yml up -d --wait

  2. The report was already stored once, under those stock settings.
     openvoxdb deduplicates reports by content hash, and the seed is
     deterministic, so re-running it submits an identical report that
     openvoxdb discards as a duplicate - it cannot repair what the first
     run stored wrongly. Start openvoxdb from an empty volume and seed
     again`, node.Certname, node.Status, demodata.Instant.Format("2006-01-02"))
	}

	fmt.Printf("    verified: every node present, and changed runs kept their resource events\n")
	return nil
}

// countSeeded counts how many of nodes belong to the demo fleet.
func countSeeded(nodes []openvoxdb.Node) int {
	ours := make(map[string]bool, len(demodata.Fleet))
	for _, n := range demodata.Fleet {
		ours[n.Certname] = true
	}
	var n int
	for _, node := range nodes {
		if ours[node.Certname] {
			n++
		}
	}
	return n
}

// eventCount returns how many resource events openvoxdb holds for
// certname.
func eventCount(ctx context.Context, client *openvoxdb.Client, certname string) (int, error) {
	rows, err := client.Query(ctx, fmt.Sprintf("events[count()] { certname = %q }", certname))
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	count, ok := rows[0]["count"].(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected count shape in openvoxdb response: %v", rows[0])
	}
	return int(count), nil
}

// warnAboutForeignNodes reports nodes openvoxdb holds that the seed did
// not write.
//
// The seed can converge its own data, but it cannot converge somebody
// else's: a node left behind by `make openvox-test` or by an earlier
// experiment stays in openvoxdb, and it will appear in every screenshot
// of a fleet-wide view alongside the demo fleet. That is worth saying out
// loud rather than discovering in a published image.
//
// It warns rather than deletes. Deactivating nodes this tool did not
// create would be a destructive act on a developer's own stack, and the
// fix - starting openvoxdb clean - is theirs to choose.
func warnAboutForeignNodes(ctx context.Context, client *openvoxdb.Client) error {
	nodes, err := client.Nodes(ctx)
	if err != nil {
		return fmt.Errorf("checking for pre-existing nodes: %w", err)
	}

	ours := make(map[string]bool, len(demodata.Fleet))
	for _, n := range demodata.Fleet {
		ours[n.Certname] = true
	}

	var foreign []string
	for _, n := range nodes {
		if !ours[n.Certname] {
			foreign = append(foreign, n.Certname)
		}
	}
	if len(foreign) == 0 {
		return nil
	}

	fmt.Printf("\n    warning: openvoxdb holds %d node(s) this seed did not create:\n", len(foreign))
	for _, certname := range foreign {
		fmt.Printf("      %s\n", certname)
	}
	fmt.Print("    They will appear in fleet-wide screenshots alongside the demo fleet.\n" +
		"    --reset deliberately leaves them alone; it only removes what this tool\n" +
		"    created. Remove one yourself with openvoxdb's admin API:\n" +
		"      curl --cert ... -d '{\"command\":\"delete\",\"version\":1,\"payload\":{\"certname\":\"NAME\"}}' \\\n" +
		"           https://localhost:8081/pdb/admin/v1/cmd\n\n")
	return nil
}

// submitFacts sends a "replace facts" command: the node's Facter facts
// plus its package inventory, which openvoxdb stores as a separate
// entity fed from this same command.
func submitFacts(ctx context.Context, client *openvoxdb.Client, node demodata.Node) error {
	pkgs := demodata.PackagesFor(node)

	// openvoxdb's package_inventory is an array of [name, version,
	// provider] triples, not objects.
	inventory := make([][3]string, 0, len(pkgs))
	for _, p := range pkgs {
		inventory = append(inventory, [3]string{p.Name, p.Version, p.Provider})
	}

	payload := map[string]any{
		"certname":           node.Certname,
		"environment":        node.Environment,
		"producer_timestamp": timestamp(demodata.Ago(node.ReportAge)),
		"producer":           producer,
		"values":             factValues(node),
		"package_inventory":  inventory,
	}
	return client.Command(ctx, "replace facts", 5, node.Certname, payload)
}

// factValues is the node's fact set. It covers what the console actually
// reads - the structured os fact, networking.fqdn, and the
// console_package_inventory companion fact - plus enough of the ordinary
// Facter surface that a node detail page looks like a real one and match
// rules have something to match on.
func factValues(node demodata.Node) map[string]any {
	p := node.Platform

	values := map[string]any{
		"os": map[string]any{
			"name":         p.Name,
			"family":       p.Family,
			"architecture": p.Architecture,
			"hardware":     p.Architecture,
			"release": map[string]any{
				"full":  p.Release,
				"major": p.Major,
			},
		},
		"networking": map[string]any{
			"fqdn":      node.FQDN(),
			"hostname":  hostname(node.Certname),
			"domain":    domainOf(node.Certname),
			"ip":        node.IPAddress,
			"interface": defaultInterface(p),
			"interfaces": map[string]any{
				defaultInterface(p): map[string]any{
					"ip":      node.IPAddress,
					"netmask": "255.255.255.0",
					"mtu":     1500,
				},
				"lo": map[string]any{
					"ip":      "127.0.0.1",
					"netmask": "255.0.0.0",
					"mtu":     65536,
				},
			},
		},
		"kernel":             p.Kernel,
		"architecture":       p.Architecture,
		"osfamily":           p.Family,
		"operatingsystem":    p.Name,
		"puppet_environment": node.Environment,
		"aio_agent_version":  "8.10.0",
		"puppetversion":      "8.10.0",
		"facterversion":      "4.10.0",
		"clientcert":         node.Certname,
		"role":               node.Role,
		"virtual":            "kvm",
		"is_virtual":         true,
		"processors": map[string]any{
			"count":  processorCount(node.Role),
			"models": []string{"AMD EPYC 7713 64-Core Processor"},
		},
		"memory": map[string]any{
			"system": map[string]any{
				"total":       memoryFor(node.Role),
				"total_bytes": memoryBytesFor(node.Role),
			},
		},
		"system_uptime": map[string]any{
			"days":    node.UptimeDays,
			"hours":   node.UptimeDays * 24,
			"seconds": node.UptimeDays * 86400,
			"uptime":  fmt.Sprintf("%d days", node.UptimeDays),
		},
		"timezone": "UTC",
		// The companion fact the console's own fact scripts emit
		// alongside the package inventory. Format 2 is what the current
		// scripts report; Sources is empty because these fabricated
		// packages have no binary/source name divergence to record.
		"console_package_inventory": map[string]any{
			"format":  2,
			"sources": map[string][]string{},
		},
	}

	if p.Family == "windows" {
		values["kernel"] = "windows"
		values["virtual"] = "hyperv"
		delete(values, "osfamily")
		values["osfamily"] = "windows"
	}

	return values
}

// submitCatalog sends a minimal but well-formed "replace catalog". The
// console does not read catalogs directly, but a node with facts and
// reports and no catalog is an odd shape for openvoxdb to hold, and the
// catalog is what gives a node its code_id and catalog_uuid.
func submitCatalog(ctx context.Context, client *openvoxdb.Client, node demodata.Node) error {
	produced := demodata.Ago(node.ReportAge)

	payload := map[string]any{
		"certname":           node.Certname,
		"version":            fmt.Sprintf("%d", produced.Unix()),
		"environment":        node.Environment,
		"transaction_uuid":   stableUUID(node.Certname, "catalog"),
		"catalog_uuid":       stableUUID(node.Certname, "catalog-uuid"),
		"code_id":            codeIDFor(node.Environment),
		"producer_timestamp": timestamp(produced),
		"producer":           producer,
		"job_id":             nil,
		"resources": []map[string]any{
			{
				"type":       "Stage",
				"title":      "main",
				"exported":   false,
				"tags":       []string{"stage"},
				"parameters": map[string]any{"name": "main"},
			},
			{
				"type":       "Class",
				"title":      "Settings",
				"exported":   false,
				"tags":       []string{"class", "settings"},
				"parameters": map[string]any{},
			},
			{
				"type":     "Class",
				"title":    classForRole(node.Role),
				"exported": false,
				"tags":     []string{"class", node.Role},
				"parameters": map[string]any{
					"environment": node.Environment,
				},
			},
		},
		"edges": []map[string]any{
			{
				"source":       map[string]string{"type": "Stage", "title": "main"},
				"target":       map[string]string{"type": "Class", "title": "Settings"},
				"relationship": "contains",
			},
			{
				"source":       map[string]string{"type": "Stage", "title": "main"},
				"target":       map[string]string{"type": "Class", "title": classForRole(node.Role)},
				"relationship": "contains",
			},
		},
	}
	return client.Command(ctx, "replace catalog", 9, node.Certname, payload)
}

// submitReport sends a "store report" command. This is what gives a node
// its latest_report_status, report_timestamp and report_environment -
// the fields the dashboard and node list are built on - and its
// resource events, which the report detail page lists.
func submitReport(ctx context.Context, client *openvoxdb.Client, node demodata.Node) error {
	end := demodata.Ago(node.ReportAge)
	start := end.Add(-runDuration(node))

	resources, logs, metrics := reportBody(node, end)

	payload := map[string]any{
		"certname":              node.Certname,
		"environment":           node.Environment,
		"report_format":         12,
		"puppet_version":        "8.10.0",
		"transaction_uuid":      stableUUID(node.Certname, "report"),
		"catalog_uuid":          stableUUID(node.Certname, "catalog-uuid"),
		"code_id":               codeIDFor(node.Environment),
		"job_id":                nil,
		"cached_catalog_status": "not_used",
		"configuration_version": fmt.Sprintf("%d", end.Unix()),
		"start_time":            timestamp(start),
		"end_time":              timestamp(end),
		"producer_timestamp":    timestamp(end),
		"producer":              producer,
		"noop":                  false,
		"noop_pending":          false,
		"corrective_change":     node.CorrectiveChange,
		"status":                string(node.Status),
		"resources":             resources,
		"metrics":               metrics,
		"logs":                  logs,
	}
	return client.Command(ctx, "store report", 8, node.Certname, payload)
}

// timestamp renders t the way openvoxdb's commands expect it. Everything
// the seed writes derives from the fixed demo instant, so these strings
// are identical run to run.
func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// stableUUID derives a UUID-shaped identifier from fixed inputs. A random
// UUID would change every run and take every screenshot showing one with
// it, which is exactly what the determinism requirement forbids.
func stableUUID(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%s-%s-%s-%s-%s", sum[0:8], sum[8:12], sum[12:16], sum[16:20], sum[20:32])
}

// codeIDFor renders the code_id a catalog and its report carry. Real
// ones are urn:puppet:code-id:1:<sha1>:<environment>, so this matches
// that shape rather than inventing one.
func codeIDFor(environment string) string {
	return fmt.Sprintf("urn:puppet:code-id:1:%s:%s", stableSHA("code", environment), environment)
}

// stableSHA is the full 40-character hex digest of its inputs, for the
// identifiers that are shaped like a git sha rather than a UUID.
func stableSHA(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hostname(certname string) string {
	for i := 0; i < len(certname); i++ {
		if certname[i] == '.' {
			return certname[:i]
		}
	}
	return certname
}

func domainOf(certname string) string {
	for i := 0; i < len(certname); i++ {
		if certname[i] == '.' {
			return certname[i+1:]
		}
	}
	return ""
}

func defaultInterface(p demodata.Platform) string {
	if p.Family == "windows" {
		return "Ethernet"
	}
	return "ens192"
}

func classForRole(role string) string {
	return "Profile::" + capitalize(role)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

func processorCount(role string) int {
	switch role {
	case "database":
		return 16
	case "ci":
		return 8
	case "web", "api", "application":
		return 4
	default:
		return 2
	}
}

func memoryFor(role string) string {
	switch role {
	case "database":
		return "64.00 GiB"
	case "ci":
		return "32.00 GiB"
	case "web", "api", "application":
		return "16.00 GiB"
	default:
		return "8.00 GiB"
	}
}

func memoryBytesFor(role string) int64 {
	switch role {
	case "database":
		return 68719476736
	case "ci":
		return 34359738368
	case "web", "api", "application":
		return 17179869184
	default:
		return 8589934592
	}
}

// runDuration is how long a node's Puppet run took. It varies by outcome
// and role so that a report list does not show the same figure on every
// row, and is derived rather than random so it is stable run to run.
func runDuration(node demodata.Node) time.Duration {
	base := 12 * time.Second
	switch node.Status {
	case demodata.StatusChanged:
		base = 24 * time.Second
	case demodata.StatusFailed:
		base = 31 * time.Second
	}
	return base + time.Duration(len(node.Certname)%7)*time.Second
}
