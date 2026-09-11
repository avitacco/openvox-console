package openvoxdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// These integration tests expect `make openvox-up` running and at least
// one real agent run recorded via `make openvox-test` (see
// internal/openvoxdb/client_test.go's testClient for the skip behavior).
const testCertname = "openvox-testing-agent"

// TestNode_DecodesLatestReportCorrectiveChange is a pure decode test (no
// live openvoxdb needed) confirming the new field parses correctly from
// a realistic nodes{} response body - see design.md in
// add-dashboard-fleet-status-stats for why no PQL query change was
// needed to add this field.
func TestNode_DecodesLatestReportCorrectiveChange(t *testing.T) {
	body := `[
		{"certname": "corrected-node.example.com", "latest_report_status": "changed", "latest_report_corrective_change": true, "report_timestamp": "2026-08-26T12:00:00Z"},
		{"certname": "intentional-node.example.com", "latest_report_status": "changed", "latest_report_corrective_change": false, "report_timestamp": "2026-08-26T12:00:00Z"},
		{"certname": "untracked-node.example.com", "latest_report_status": "changed", "latest_report_corrective_change": null, "report_timestamp": "2026-08-26T12:00:00Z"}
	]`

	var nodes []Node
	if err := json.Unmarshal([]byte(body), &nodes); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(nodes))
	}

	if nodes[0].LatestReportCorrectiveChange == nil || !*nodes[0].LatestReportCorrectiveChange {
		t.Errorf("corrected-node: LatestReportCorrectiveChange = %v, want true", nodes[0].LatestReportCorrectiveChange)
	}
	if nodes[1].LatestReportCorrectiveChange == nil || *nodes[1].LatestReportCorrectiveChange {
		t.Errorf("intentional-node: LatestReportCorrectiveChange = %v, want false", nodes[1].LatestReportCorrectiveChange)
	}
	if nodes[2].LatestReportCorrectiveChange != nil {
		t.Errorf("untracked-node: LatestReportCorrectiveChange = %v, want nil", nodes[2].LatestReportCorrectiveChange)
	}
}

// TestPackage_Decodes is a pure decode test (no live openvoxdb needed)
// confirming Package parses correctly from a realistic package_inventory
// response body.
func TestPackage_Decodes(t *testing.T) {
	body := `[
		{"certname": "node1.example.com", "package_name": "openssl", "provider": "apt", "version": "3.0.2-0ubuntu1.10"},
		{"certname": "node2.example.com", "package_name": "openssl", "provider": "apt", "version": "3.0.2-0ubuntu1.15"}
	]`

	var packages []Package
	if err := json.Unmarshal([]byte(body), &packages); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("got %d packages, want 2", len(packages))
	}
	want := Package{Certname: "node1.example.com", PackageName: "openssl", Provider: "apt", Version: "3.0.2-0ubuntu1.10"}
	if packages[0] != want {
		t.Errorf("packages[0] = %+v, want %+v", packages[0], want)
	}
}

// fakeQueryServer records the PQL query string from each request body and
// responds with body - lets SearchPackages' generated PQL be asserted
// without a live openvoxdb.
func fakeQueryServer(t *testing.T, body string) (*Client, *string) {
	t.Helper()
	var gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotQuery = req.Query
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return &Client{baseURL: ts.URL, http: ts.Client()}, &gotQuery
}

func TestSearchPackages_NameOnly(t *testing.T) {
	client, gotQuery := fakeQueryServer(t, `[]`)

	if _, err := client.SearchPackages(context.Background(), "openssl", nil); err != nil {
		t.Fatalf("SearchPackages() error: %v", err)
	}
	want := `package_inventory { package_name = "openssl" }`
	if *gotQuery != want {
		t.Errorf("PQL query = %q, want %q", *gotQuery, want)
	}
}

func TestSearchPackages_NameAndVersion(t *testing.T) {
	client, gotQuery := fakeQueryServer(t, `[]`)

	version := "3.0.2-0ubuntu1.10"
	if _, err := client.SearchPackages(context.Background(), "openssl", &version); err != nil {
		t.Fatalf("SearchPackages() error: %v", err)
	}
	want := `package_inventory { package_name = "openssl" and version = "3.0.2-0ubuntu1.10" }`
	if *gotQuery != want {
		t.Errorf("PQL query = %q, want %q", *gotQuery, want)
	}
}

func TestNodes(t *testing.T) {
	client := testClient(t)

	nodes, err := client.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes() error: %v", err)
	}

	var found *Node
	for i := range nodes {
		if nodes[i].Certname == testCertname {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Nodes() did not include %q; run `make openvox-test` first", testCertname)
	}
	if found.LatestReportStatus == nil {
		t.Error("expected LatestReportStatus to be set for a node with a recorded report")
	}
	if found.ReportTimestamp == nil {
		t.Error("expected ReportTimestamp to be set for a node with a recorded report")
	}
}

func TestNodes_UsesUnfilteredQuery(t *testing.T) {
	client, gotQuery := fakeQueryServer(t, `[]`)

	if _, err := client.Nodes(context.Background()); err != nil {
		t.Fatalf("Nodes() error: %v", err)
	}
	want := `nodes {}`
	if *gotQuery != want {
		t.Errorf("Nodes() PQL query = %q, want %q", *gotQuery, want)
	}
}

// TestNodeByCertname_FindsAlreadyDeactivatedNode is the live counterpart
// to TestDeactivateNode_Success: confirms NodeByCertname (unlike Nodes)
// still finds a node after deactivating it, using a synthetic certname
// per run so this doesn't depend on test ordering or another test's
// state.
func TestNodeByCertname_FindsAlreadyDeactivatedNode(t *testing.T) {
	client := testClient(t)
	certname := "test-nodebycertname-" + time.Now().UTC().Format("20060102150405.000000000")

	if err := client.DeactivateNode(context.Background(), certname); err != nil {
		t.Fatalf("DeactivateNode() error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		node, err := client.NodeByCertname(context.Background(), certname)
		if err == nil && node.Deactivated != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("NodeByCertname() never showed %q as deactivated (last err: %v)", certname, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestNodeByCertname_UnknownCertnameReturnsErrNodeNotFound(t *testing.T) {
	client := testClient(t)

	_, err := client.NodeByCertname(context.Background(), "this-certname-truly-does-not-exist-anywhere")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("NodeByCertname() error = %v, want ErrNodeNotFound", err)
	}
}

func TestFacts(t *testing.T) {
	client := testClient(t)

	facts, err := client.Facts(context.Background(), testCertname)
	if err != nil {
		t.Fatalf("Facts() error: %v", err)
	}
	if len(facts) == 0 {
		t.Fatal("Facts() returned no facts")
	}

	var hasOS bool
	for _, f := range facts {
		if f.Name == "os" {
			hasOS = true
			break
		}
	}
	if !hasOS {
		t.Error("expected an 'os' fact among the results")
	}
}

func TestFactCertnames(t *testing.T) {
	client := testClient(t)

	facts, err := client.Facts(context.Background(), testCertname)
	if err != nil {
		t.Fatalf("Facts() error: %v", err)
	}
	var factName string
	var factValue string
	for _, f := range facts {
		var s string
		if err := json.Unmarshal(f.Value, &s); err == nil && s != "" {
			factName, factValue = f.Name, s
			break
		}
	}
	if factName == "" {
		t.Fatal("no string-valued fact found to test FactCertnames with")
	}

	certnames, err := client.FactCertnames(context.Background(), factName, factValue)
	if err != nil {
		t.Fatalf("FactCertnames() error: %v", err)
	}
	var found bool
	for _, c := range certnames {
		if c == testCertname {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FactCertnames(%q, %q) = %v, want it to include %q", factName, factValue, certnames, testCertname)
	}
}

func TestReports(t *testing.T) {
	client := testClient(t)

	reports, err := client.Reports(context.Background(), testCertname)
	if err != nil {
		t.Fatalf("Reports() error: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("Reports() returned no reports; run `make openvox-test` first")
	}

	for i := 1; i < len(reports); i++ {
		if reports[i].StartTime.After(reports[i-1].StartTime) {
			t.Errorf("Reports() not sorted descending by StartTime at index %d", i)
		}
	}
}

func TestEvents(t *testing.T) {
	client := testClient(t)

	reports, err := client.Reports(context.Background(), testCertname)
	if err != nil {
		t.Fatalf("Reports() error: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("no reports to fetch events for; run `make openvox-test` first")
	}

	// The first (most recent) report is expected to be the one from the
	// site.pp manifest change, which produced a real resource event.
	events, err := client.Events(context.Background(), reports[0].Hash)
	if err != nil {
		t.Fatalf("Events() error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Events() returned no events for the most recent report")
	}
	if events[0].ResourceType == "" {
		t.Error("expected a non-empty ResourceType on the event")
	}
}
