package nodeagent

import (
	"context"
	"testing"
	"time"
)

// TestClient_ReconnectsAfterConnectionDrop proves nats.go's built-in
// reconnection (configured in Run - see client.go) actually recovers a
// node-agent's subscription after its connection is severed server-side,
// without the client process restarting.
func TestClient_ReconnectsAfterConnectionDrop(t *testing.T) {
	transport, ca := startTestTransport(t)
	certPath, keyPath := ca.Issue(t, "web01.example.com", false)

	client := New(Config{
		TransportAddr: transport.Addr(),
		CertFile:      certPath,
		KeyFile:       keyPath,
		CAFile:        ca.PEMFile(t),
	}, NewHandler(fakeRunner("ok", "", 0, nil), "puppet", t.TempDir(), noFileExists), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	waitForConnected := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && !transport.Registry().Lookup("web01.example.com") {
			time.Sleep(20 * time.Millisecond)
		}
		if !transport.Registry().Lookup("web01.example.com") {
			t.Fatal("node-agent never (re)connected to the test transport")
		}
	}

	waitForConnected()

	// Sever every connection on the server side - simulates a dropped
	// TCP connection/network blip, not a client-initiated disconnect.
	// Reconnection (via nats.go's automatic reconnection - see
	// client.go's Run) can happen fast enough that a transient
	// "currently disconnected" window isn't reliably observable here;
	// what matters is that it recovers without the client process
	// restarting, which waitForConnected below confirms.
	if err := transport.CloseAllConnections(); err != nil {
		t.Fatalf("CloseAllConnections() error: %v", err)
	}
	waitForConnected()

	// And the resubscription must actually work, not just the raw
	// connection.
	req := requestPayload(t, "puppet", actionRun, nil)
	resp, err := transport.Dispatch(context.Background(), "web01.example.com", req, 3*time.Second)
	if err != nil {
		t.Fatalf("Dispatch() after reconnect error: %v", err)
	}
	wr := decodeWireResponse(t, resp)
	if wr.Result == nil || wr.Result.Output.Stdout != "ok" {
		t.Errorf("Result after reconnect = %+v, want Stdout %q", wr.Result, "ok")
	}
}
