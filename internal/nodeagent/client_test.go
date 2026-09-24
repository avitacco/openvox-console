package nodeagent

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// startTestTransport builds and starts a real nodetransport.Server on an
// OS-assigned port, returning it and the CA it trusts.
func startTestTransport(t *testing.T) (*nodetransport.Server, *testca.CA) {
	t.Helper()

	ca := testca.NewCA(t)
	serverCert, serverKey := ca.Issue(t, "test-node-transport", true)

	s, err := nodetransport.New(nodetransport.Config{
		ListenAddr: "127.0.0.1:0",
		CertFile:   serverCert,
		KeyFile:    serverKey,
		CAFile:     ca.PEMFile(t),
	}, messaging.Config{})
	if err != nil {
		t.Fatalf("nodetransport.New() error: %v", err)
	}
	t.Cleanup(s.Close)

	return s, ca
}

func TestClient_ConnectsAndExecutesADispatchRequest(t *testing.T) {
	transport, ca := startTestTransport(t)
	certPath, keyPath := ca.Issue(t, "web01.example.com", false)

	client := New(Config{
		TransportAddr: transport.Addr(),
		CertFile:      certPath,
		KeyFile:       keyPath,
		CAFile:        ca.PEMFile(t),
	}, NewHandler(fakeRunner("changes applied", "", 2, nil), "puppet", t.TempDir(), noFileExists), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !transport.Registry().Lookup("web01.example.com") {
		time.Sleep(20 * time.Millisecond)
	}
	if !transport.Registry().Lookup("web01.example.com") {
		t.Fatal("node-agent never connected to the test transport")
	}

	req := requestPayload(t, "puppet", actionRun, nil)
	resp, err := transport.Dispatch(context.Background(), "web01.example.com", req, 3*time.Second)
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}

	wr := decodeWireResponse(t, resp)
	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty", wr.Error)
	}
	if wr.Result == nil || wr.Result.Output.Stdout != "changes applied" {
		t.Errorf("Result = %+v, want Stdout %q", wr.Result, "changes applied")
	}
}

// TestClient_DispatchRejectsPackageInventoryRequestDuringInFlightRun
// confirms the existing single-in-flight guard (client.go's dispatch)
// covers the new package-inventory actions with no changes needed there
// (see tasks.md's task 2.5 in add-package-inventory-toggle). Calls
// dispatch directly rather than through a real transport round-trip: a
// real nats.Conn.Subscribe callback already only ever receives messages
// one at a time (nats.go delivers to a single subscription serially,
// confirmed while writing this test - an earlier version of it tried to
// race two real Dispatch calls through the transport and the second
// simply blocked in NATS's own delivery queue rather than ever reaching
// dispatch concurrently), so busy's CompareAndSwap can only actually
// matter if something calls dispatch itself out of band - this test
// exercises that guard directly instead of relying on delivery
// semantics this client doesn't control.
func TestClient_DispatchRejectsPackageInventoryRequestDuringInFlightRun(t *testing.T) {
	var handlerCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	client := &Client{
		logger: slog.Default(),
		handler: func(ctx context.Context, payload []byte) []byte {
			handlerCalls.Add(1)
			close(started)
			<-release
			return []byte(`{"result":{}}`)
		},
	}

	go client.dispatch(context.Background(), &nats.Msg{Subject: "test.dispatch"})

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first dispatch never started executing")
	}

	// Second call while the first is still in flight - a
	// package-inventory status request, arbitrary content since the
	// busy guard rejects it before the handler (and thus the actual
	// action) is ever reached.
	client.dispatch(context.Background(), &nats.Msg{Subject: "test.dispatch"})

	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler invoked %d times during the overlap, want exactly 1 - the second dispatch should have been rejected as busy before reaching it", got)
	}

	close(release)
}
