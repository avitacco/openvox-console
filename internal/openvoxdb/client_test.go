package openvoxdb

import (
	"context"
	"os"
	"testing"
	"time"
)

// testClient builds a Client from CONSOLE_TEST_OPENVOXDB_* env vars,
// skipping the test if they aren't set - mirrors internal/persistence's
// testDSN pattern for tests that need a real dependency.
func testClient(t *testing.T) *Client {
	t.Helper()

	url := os.Getenv("CONSOLE_TEST_OPENVOXDB_URL")
	certFile := os.Getenv("CONSOLE_TEST_OPENVOXDB_CERT_FILE")
	keyFile := os.Getenv("CONSOLE_TEST_OPENVOXDB_KEY_FILE")
	caFile := os.Getenv("CONSOLE_TEST_OPENVOXDB_CA_FILE")
	if url == "" || certFile == "" || keyFile == "" || caFile == "" {
		t.Skip("CONSOLE_TEST_OPENVOXDB_* not set; skipping integration test")
	}

	client, err := NewClient(url, certFile, keyFile, caFile)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	return client
}

func TestQuery_Success(t *testing.T) {
	client := testClient(t)

	rows, err := client.Query(context.Background(), "nodes {}")
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("Query() returned no nodes; expected at least one from `make openvox-test`")
	}
	if _, ok := rows[0]["certname"]; !ok {
		t.Errorf("Query() row missing certname field: %+v", rows[0])
	}
}

func TestQuery_MalformedNotSent(t *testing.T) {
	client := testClient(t)

	_, err := client.Query(context.Background(), "nodes {")
	if err == nil {
		t.Fatal("Query() with malformed PQL returned nil error, want an error")
	}
}

// TestDeactivateNode_Success uses a synthetic, never-before-seen
// certname rather than testCertname (the shared `make openvox-test`
// fixture node) - deactivating that one would break every other test
// expecting it to still be an active, reporting node. Confirmed live
// (see design.md in add-node-deletion) that openvoxdb creates a new,
// already-deactivated stub node for an unknown certname rather than
// erroring - exactly what this test exercises and asserts.
func TestDeactivateNode_Success(t *testing.T) {
	client := testClient(t)
	certname := "test-deactivate-" + time.Now().UTC().Format("20060102150405.000000000")

	if err := client.DeactivateNode(context.Background(), certname); err != nil {
		t.Fatalf("DeactivateNode() error: %v", err)
	}

	// openvoxdb processes commands asynchronously - poll briefly rather
	// than assume it's already applied the instant the HTTP call
	// returns. NodeByCertname, not Nodes: confirmed live that Nodes'
	// collection query excludes a deactivated node unconditionally, with
	// no override - only the single-node lookup route NodeByCertname
	// uses still returns it (see design.md).
	deadline := time.Now().Add(5 * time.Second)
	for {
		node, err := client.NodeByCertname(context.Background(), certname)
		if err != nil {
			t.Fatalf("NodeByCertname() error: %v", err)
		}
		if node.Deactivated != nil {
			return // success
		}
		if time.Now().After(deadline) {
			t.Fatalf("node %q did not show as deactivated within 5s", certname)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestQuery_Unreachable(t *testing.T) {
	certFile := os.Getenv("CONSOLE_TEST_OPENVOXDB_CERT_FILE")
	keyFile := os.Getenv("CONSOLE_TEST_OPENVOXDB_KEY_FILE")
	caFile := os.Getenv("CONSOLE_TEST_OPENVOXDB_CA_FILE")
	if certFile == "" || keyFile == "" || caFile == "" {
		t.Skip("CONSOLE_TEST_OPENVOXDB_* not set; skipping integration test")
	}

	client, err := NewClient("https://127.0.0.1:1", certFile, keyFile, caFile)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, err = client.Query(context.Background(), "nodes {}")
	if err == nil {
		t.Fatal("Query() against an unreachable host returned nil error, want an error")
	}
}
