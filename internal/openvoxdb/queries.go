package openvoxdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Node is a single entry from openvoxdb's node inventory.
type Node struct {
	Certname           string     `json:"certname"`
	LatestReportStatus *string    `json:"latest_report_status"`
	ReportTimestamp    *time.Time `json:"report_timestamp"`
	// LatestReportCorrectiveChange is nil when Puppet's corrective-change
	// tracking isn't enabled for this node (or there's no report to
	// judge), true when the latest report corrected unexpected drift,
	// and false when it changed with no corrective drift involved. This
	// field is already present on every real nodes{} response - no PQL
	// query change was needed to add it, only this struct field.
	LatestReportCorrectiveChange *bool `json:"latest_report_corrective_change"`
	// Deactivated and Expired are always nil on a row returned by Nodes -
	// openvoxdb's nodes query entity unconditionally excludes deactivated
	// and expired nodes with no documented way to override that from a
	// PQL query (confirmed live and against PuppetDB's own docs - see
	// design.md in add-node-deletion; even an explicit
	// `certname = "..."` or `deactivated is not null` filter still
	// returns nothing for a deactivated node). They're only ever
	// populated on a Node returned by the separate NodeByCertname, which
	// uses openvoxdb's single-node lookup route instead - that route
	// does not apply the same exclusion.
	Deactivated *time.Time `json:"deactivated"`
	Expired     *time.Time `json:"expired"`
}

// Nodes returns every active node known to openvoxdb - deactivated and
// expired nodes are excluded unconditionally by openvoxdb itself, not
// by anything this query adds (see the Node.Deactivated doc comment).
func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	var nodes []Node
	if err := c.query(ctx, "nodes {}", &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// ErrNodeNotFound is returned by NodeByCertname when openvoxdb has no
// record of that certname at all - as opposed to a certname it knows
// about but that's deactivated, which NodeByCertname still returns
// successfully (see its own doc comment).
var ErrNodeNotFound = errors.New("node not found")

// NodeByCertname fetches a single node by certname via openvoxdb's
// direct node-lookup route (GET /pdb/query/v4/nodes/<certname>), not the
// general PQL query endpoint Nodes uses - confirmed live (see design.md
// in add-node-deletion) that this is the one route that returns a
// deactivated node's record rather than excluding it; a certname
// openvoxdb has never heard of still 404s, translated here to
// ErrNodeNotFound.
func (c *Client) NodeByCertname(ctx context.Context, certname string) (*Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/pdb/query/v4/nodes/"+url.PathEscape(certname), nil)
	if err != nil {
		return nil, fmt.Errorf("build openvoxdb node lookup request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openvoxdb unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openvoxdb unreachable: read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNodeNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openvoxdb node lookup failed (status %d): %s", resp.StatusCode, describeError(body))
	}

	var node Node
	if err := json.Unmarshal(body, &node); err != nil {
		return nil, fmt.Errorf("parse openvoxdb response: %w", err)
	}
	return &node, nil
}

// Fact is a single fact reported by a node. Value is left as raw JSON
// since a fact's value can be a string, number, bool, array, or object.
type Fact struct {
	Certname string          `json:"certname"`
	Name     string          `json:"name"`
	Value    json.RawMessage `json:"value"`
}

// Facts returns every fact reported by the node named certname.
func (c *Client) Facts(ctx context.Context, certname string) ([]Fact, error) {
	var facts []Fact
	pql := fmt.Sprintf("facts { certname = %s }", pqlString(certname))
	if err := c.query(ctx, pql, &facts); err != nil {
		return nil, err
	}
	return facts, nil
}

// FactCertnames returns the certnames of every node reporting the fact
// named name with exactly value.
func (c *Client) FactCertnames(ctx context.Context, name, value string) ([]string, error) {
	var facts []Fact
	pql := fmt.Sprintf("facts { name = %s and value = %s }", pqlString(name), pqlString(value))
	if err := c.query(ctx, pql, &facts); err != nil {
		return nil, err
	}
	certnames := make([]string, 0, len(facts))
	for _, f := range facts {
		certnames = append(certnames, f.Certname)
	}
	return certnames, nil
}

// Report is a single catalog run recorded for a node.
type Report struct {
	Hash        string    `json:"hash"`
	Certname    string    `json:"certname"`
	Status      string    `json:"status"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	ReceiveTime time.Time `json:"receive_time"`
}

// Reports returns the report history for the node named certname, most
// recent first.
func (c *Client) Reports(ctx context.Context, certname string) ([]Report, error) {
	var reports []Report
	pql := fmt.Sprintf("reports { certname = %s }", pqlString(certname))
	if err := c.query(ctx, pql, &reports); err != nil {
		return nil, err
	}
	sortReportsDescending(reports)
	return reports, nil
}

func sortReportsDescending(reports []Report) {
	// Small result sets (see design.md - no pagination for this phase), so
	// a simple insertion sort keeps this dependency-free and readable.
	for i := 1; i < len(reports); i++ {
		for j := i; j > 0 && reports[j].StartTime.After(reports[j-1].StartTime); j-- {
			reports[j], reports[j-1] = reports[j-1], reports[j]
		}
	}
}

// Package is a single package known to be installed on a node, as
// reported by openvoxdb's package_inventory query entity.
type Package struct {
	Certname    string `json:"certname"`
	PackageName string `json:"package_name"`
	Provider    string `json:"provider"`
	Version     string `json:"version"`
}

// NodePackages returns every package openvoxdb knows to be installed on
// the node named certname.
func (c *Client) NodePackages(ctx context.Context, certname string) ([]Package, error) {
	var packages []Package
	pql := fmt.Sprintf("package_inventory { certname = %s }", pqlString(certname))
	if err := c.query(ctx, pql, &packages); err != nil {
		return nil, err
	}
	return packages, nil
}

// SearchPackages returns every package_inventory row for the package
// named name, optionally narrowed to an exact version.
func (c *Client) SearchPackages(ctx context.Context, name string, version *string) ([]Package, error) {
	var packages []Package
	pql := fmt.Sprintf("package_inventory { package_name = %s", pqlString(name))
	if version != nil {
		pql += fmt.Sprintf(" and version = %s", pqlString(*version))
	}
	pql += " }"
	if err := c.query(ctx, pql, &packages); err != nil {
		return nil, err
	}
	return packages, nil
}

// NodeAllFacts is every fact one node reports, as the inventory
// endpoint's bare `facts` field returns it.
type NodeAllFacts struct {
	Certname string         `json:"certname"`
	Facts    map[string]any `json:"facts"`
}

// FleetFacts returns every node's complete fact set in a single query.
// Facts() fetches one node at a time, so evaluating every group against
// every node that way is an N+1 - a page of 25 groups over 1000 nodes
// would be 25,000 queries. This is one, and callers match in memory.
func (c *Client) FleetFacts(ctx context.Context) ([]NodeAllFacts, error) {
	var facts []NodeAllFacts
	if err := c.query(ctx, "inventory[certname, facts] {}", &facts); err != nil {
		return nil, err
	}
	return facts, nil
}

// PackageVersion is one distinct package-name/version pair openvoxdb
// holds across the fleet.
type PackageVersion struct {
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
}

// NodePackageCount is how many package_inventory rows one node reports.
type NodePackageCount struct {
	Certname string `json:"certname"`
	Count    int    `json:"count"`
}

// PackageFilter narrows the fleet package catalogue. A zero filter
// matches everything.
//
// Certnames restricts to a set of nodes. A nil slice means "no node
// restriction"; callers with an empty-but-non-nil set (a group matching
// nothing) must not query at all, since an empty `in []` is not a
// meaningful condition - see packagesCatalog.
type PackageFilter struct {
	NameContains    string
	VersionContains string
	Provider        string
	Certnames       []string
}

// where renders the filter as PQL conditions, ready to precede a
// `group by`. NameContains becomes a regex match, so the caller's text
// is quoted with regexp.QuoteMeta first - otherwise a package name
// containing "+" or "." (libstdc++6, python3.12) would be read as a
// pattern and match the wrong rows, and "(" would be a syntax error.
func (f PackageFilter) where() string {
	var conds []string
	if f.NameContains != "" {
		conds = append(conds, fmt.Sprintf("package_name ~ %s", pqlString(regexp.QuoteMeta(f.NameContains))))
	}
	if f.VersionContains != "" {
		conds = append(conds, fmt.Sprintf("version ~ %s", pqlString(regexp.QuoteMeta(f.VersionContains))))
	}
	if f.Provider != "" {
		conds = append(conds, fmt.Sprintf("provider = %s", pqlString(f.Provider)))
	}
	if len(f.Certnames) > 0 {
		quoted := make([]string, 0, len(f.Certnames))
		for _, c := range f.Certnames {
			quoted = append(quoted, pqlString(c))
		}
		conds = append(conds, fmt.Sprintf("certname in [%s]", strings.Join(quoted, ", ")))
	}
	if len(conds) == 0 {
		return ""
	}
	return strings.Join(conds, " and ") + " "
}

// PackageNameCount is one package name, as reported by one provider,
// with how many package_inventory rows carry it.
type PackageNameCount struct {
	PackageName string `json:"package_name"`
	Provider    string `json:"provider"`
	Count       int    `json:"count"`
}

// ProviderCount is one package provider and how many rows it accounts
// for across the fleet.
type ProviderCount struct {
	Provider string `json:"provider"`
	Count    int    `json:"count"`
}

// PackageNameCounts returns the fleet's distinct package names matching
// filter, each with the provider reporting it and its row count.
func (c *Client) PackageNameCounts(ctx context.Context, filter PackageFilter) ([]PackageNameCount, error) {
	var counts []PackageNameCount
	pql := fmt.Sprintf("package_inventory[package_name, provider, count()] { %sgroup by package_name, provider }", filter.where())
	if err := c.query(ctx, pql, &counts); err != nil {
		return nil, err
	}
	return counts, nil
}

// PackageProviders returns every package provider in use across the
// fleet, for populating a filter control. Deliberately unfiltered: the
// choices shouldn't disappear as the user narrows the list.
func (c *Client) PackageProviders(ctx context.Context) ([]ProviderCount, error) {
	var providers []ProviderCount
	if err := c.query(ctx, "package_inventory[provider, count()] { group by provider }", &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

// PackageVersions returns every distinct package-name/version pair
// matching filter. Grouped server-side deliberately: the ungrouped row
// set is (nodes x packages), which is hundreds of thousands of rows on
// a large fleet to transfer and immediately discard, while the distinct
// pairs stay in the low thousands however many nodes report them.
//
// openvoxdb rejects `limit` and `order by` alongside `group by` (both
// return 400), so callers that want a top-N rank in Go.
func (c *Client) PackageVersions(ctx context.Context, filter PackageFilter) ([]PackageVersion, error) {
	var versions []PackageVersion
	pql := fmt.Sprintf("package_inventory[package_name, version] { %sgroup by package_name, version }", filter.where())
	if err := c.query(ctx, pql, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// PackageCountsByNode returns, for each node reporting package
// inventory, how many packages it reports. A node absent from the
// result reports none - which is how the caller tells "reporting
// nothing" apart from "not reporting at all".
func (c *Client) PackageCountsByNode(ctx context.Context) ([]NodePackageCount, error) {
	var counts []NodePackageCount
	pql := "package_inventory[certname, count()] { group by certname }"
	if err := c.query(ctx, pql, &counts); err != nil {
		return nil, err
	}
	return counts, nil
}

// NodeFact returns the fact named name reported by the node named
// certname, or nil if the node has not reported it.
func (c *Client) NodeFact(ctx context.Context, certname, name string) (*Fact, error) {
	var facts []Fact
	pql := fmt.Sprintf("facts { certname = %s and name = %s }", pqlString(certname), pqlString(name))
	if err := c.query(ctx, pql, &facts); err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, nil
	}
	return &facts[0], nil
}

// FleetPackages returns every package_inventory row openvoxdb holds,
// across all nodes, in one query. Callers wanting only active nodes
// filter against Nodes - package_inventory has no deactivation state of
// its own to filter on.
func (c *Client) FleetPackages(ctx context.Context) ([]Package, error) {
	var packages []Package
	if err := c.query(ctx, "package_inventory {}", &packages); err != nil {
		return nil, err
	}
	return packages, nil
}

// OSFact is the subset of Facter's structured os fact the console uses.
type OSFact struct {
	Name    string `json:"name"`
	Family  string `json:"family"`
	Release struct {
		Full  string `json:"full"`
		Major string `json:"major"`
	} `json:"release"`
}

// ConsolePackageInventory is the companion fact the Linux
// package-inventory fact scripts emit alongside _puppet_inventory_1 (see
// design.md in fix-package-inventory-fact-fidelity). Sources maps a
// binary apt package to [source name, source version], only for
// binaries whose source differs.
type ConsolePackageInventory struct {
	Format  int                 `json:"format"`
	Sources map[string][]string `json:"sources"`
}

// NodeFacts is one node's facts relevant to vulnerability assessment.
// Each pointer is nil when the node hasn't reported that fact.
type NodeFacts struct {
	Certname                string                   `json:"certname"`
	OS                      *OSFact                  `json:"facts.os"`
	FQDN                    *string                  `json:"facts.networking.fqdn"`
	ConsolePackageInventory *ConsolePackageInventory `json:"facts.console_package_inventory"`
}

// FleetNodeFacts returns NodeFacts for every node openvoxdb has an
// inventory record for, in one query - the inventory entity's dotted
// fact projections avoid pulling each node's whole (large) networking
// fact. As with FleetPackages, filter against Nodes for active nodes.
func (c *Client) FleetNodeFacts(ctx context.Context) ([]NodeFacts, error) {
	var facts []NodeFacts
	pql := "inventory[certname, facts.os, facts.networking.fqdn, facts.console_package_inventory] {}"
	if err := c.query(ctx, pql, &facts); err != nil {
		return nil, err
	}
	return facts, nil
}

// Event is a single resource-level event within a report.
type Event struct {
	ResourceType  string          `json:"resource_type"`
	ResourceTitle string          `json:"resource_title"`
	Property      *string         `json:"property"`
	Status        string          `json:"status"`
	OldValue      json.RawMessage `json:"old_value"`
	NewValue      json.RawMessage `json:"new_value"`
	Message       *string         `json:"message"`
	Timestamp     time.Time       `json:"timestamp"`
}

// Events returns the resource-level events for the report identified by
// hash.
func (c *Client) Events(ctx context.Context, hash string) ([]Event, error) {
	var events []Event
	pql := fmt.Sprintf("events { report = %s }", pqlString(hash))
	if err := c.query(ctx, pql, &events); err != nil {
		return nil, err
	}
	return events, nil
}
