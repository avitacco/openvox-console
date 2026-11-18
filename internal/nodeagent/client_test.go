package nodeagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
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
// confirms the single-in-flight guard (client.go's dispatch) covers the
// package-inventory actions with no changes needed there (see tasks.md's
// task 2.5 in add-package-inventory-toggle). It calls dispatch directly
// to exercise the guard by itself; that the guard is actually reached
// by a second request arriving over the transport is
// TestClient_SecondRequestOverTheTransportIsRefusedNotQueued's job.
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

// An upgrade's follow-up work runs after the reply, holds the client
// busy for as long as it takes, and does not block delivery of later
// requests - they are refused, not queued behind it.
func TestClient_AfterReplyHoldsBusyWithoutBlockingDelivery(t *testing.T) {
	var handlerCalls atomic.Int32
	holding := make(chan struct{})
	release := make(chan struct{})
	armed := true

	client := &Client{
		logger: slog.Default(),
		handler: func(context.Context, []byte) []byte {
			handlerCalls.Add(1)
			return []byte(`{"result":{}}`)
		},
	}
	client.SetAfterReply(func() func(context.Context) {
		if !armed {
			return nil
		}
		armed = false
		return func(context.Context) {
			close(holding)
			<-release
		}
	})

	// Returns although the follow-up work is still running.
	client.dispatch(context.Background(), &nats.Msg{Subject: "test.dispatch"})
	select {
	case <-holding:
	case <-time.After(3 * time.Second):
		t.Fatal("the after-reply work never ran")
	}

	client.dispatch(context.Background(), &nats.Msg{Subject: "test.dispatch"})
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler ran %d times while the after-reply work held the client, want 1", got)
	}

	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for client.busy.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	client.dispatch(context.Background(), &nats.Msg{Subject: "test.dispatch"})
	if got := handlerCalls.Load(); got != 2 {
		t.Errorf("handler ran %d times after the hold ended, want 2", got)
	}
}

// blockingRunHandler answers a Puppet run only once release is closed,
// signalling started when it begins, and answers a build-status request
// at once.
func blockingRunHandler(started chan<- struct{}, release <-chan struct{}, runs *atomic.Int32) Handler {
	return func(_ context.Context, payload []byte) []byte {
		var req requestData
		_ = json.Unmarshal(payload, &req)
		if req.Action == actionAgentStatus {
			return encodeResult(&responseData{Agent: &agentStatus{Digest: "abc", OS: "linux"}})
		}
		if runs.Add(1) == 1 {
			close(started)
		}
		<-release
		return []byte(`{"result":{}}`)
	}
}

// connectedClient runs a client with handler against a real transport
// and returns the transport once the client has subscribed.
func connectedClient(t *testing.T, name string, handler Handler) *nodetransport.Server {
	t.Helper()
	transport, ca := startTestTransport(t)
	certPath, keyPath := ca.Issue(t, name, false)
	client := New(Config{
		TransportAddr: transport.Addr(),
		CertFile:      certPath,
		KeyFile:       keyPath,
		CAFile:        ca.PEMFile(t),
	}, handler, nil)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go client.Run(ctx)

	// Connected is announced before the client subscribes, so wait for
	// a request to be answered rather than for the connection.
	status := requestPayload(t, "node-agent", actionAgentStatus, nil)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := transport.Dispatch(context.Background(), name, status, time.Second); err == nil {
			return transport
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("node-agent never answered on the test transport")
	return nil
}

// A second request arriving over the real transport while a run is in
// progress is refused as busy straight away. It used not to be: the run
// was handled on the subscription's delivery goroutine, so the second
// request sat in the client's queue until the run finished and then ran
// too, long after the console had stopped waiting for it.
func TestClient_SecondRequestOverTheTransportIsRefusedNotQueued(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	transport := connectedClient(t, "web01.example.com", blockingRunHandler(started, release, &runs))

	run := requestPayload(t, "puppet", actionRun, nil)
	first := make(chan []byte, 1)
	go func() {
		resp, _ := transport.Dispatch(context.Background(), "web01.example.com", run, 10*time.Second)
		first <- resp
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the first run never started")
	}

	begun := time.Now()
	resp, err := transport.Dispatch(context.Background(), "web01.example.com", run, 5*time.Second)
	if err != nil {
		t.Fatalf("second Dispatch() error: %v - it should have been answered, as busy", err)
	}
	if waited := time.Since(begun); waited > 2*time.Second {
		t.Errorf("the second request was answered after %s, want at once", waited)
	}
	if wr := decodeWireResponse(t, resp); !strings.HasPrefix(wr.Error, "node-agent is busy") {
		t.Errorf("second response error = %q, want a busy refusal", wr.Error)
	}

	close(release)
	select {
	case resp := <-first:
		if wr := decodeWireResponse(t, resp); wr.Error != "" {
			t.Errorf("first run error = %q, want it to complete undisturbed", wr.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first run never completed")
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("the run handler ran %d times, want 1: the refused request must not run later", got)
	}
}

// The console reads a node's build as it connects, at the same moment a
// new node's first Puppet run is dispatched. The status request is
// answered while the run is in progress, and does not make the client
// busy for a run that arrives while it is being answered.
func TestClient_BuildStatusIsAnsweredWhileBusy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	transport := connectedClient(t, "web02.example.com", blockingRunHandler(started, release, &runs))
	defer close(release)

	run := requestPayload(t, "puppet", actionRun, nil)
	go func() { _, _ = transport.Dispatch(context.Background(), "web02.example.com", run, 10*time.Second) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the run never started")
	}

	status := requestPayload(t, "node-agent", actionAgentStatus, nil)
	resp, err := transport.Dispatch(context.Background(), "web02.example.com", status, 3*time.Second)
	if err != nil {
		t.Fatalf("status Dispatch() error during a run: %v", err)
	}
	wr := decodeWireResponse(t, resp)
	if wr.Error != "" || wr.Result == nil || wr.Result.Agent == nil || wr.Result.Agent.Digest != "abc" {
		t.Errorf("status during a run = %+v (error %q), want the build", wr.Result, wr.Error)
	}
}

func TestAnsweredWhileBusy(t *testing.T) {
	cases := map[string]bool{
		`{"module":"node-agent","action":"agent_status"}`:  true,
		`{"module":"node-agent","action":"agent_upgrade"}`: false,
		`{"module":"puppet","action":"patch_status"}`:      true,
		`{"module":"puppet","action":"patch_apply"}`:       false,
		`{"module":"puppet","action":"run"}`:               false,
		`not json`:                                         false,
		``:                                                 false,
	}
	for payload, want := range cases {
		if got := answeredWhileBusy([]byte(payload)); got != want {
			t.Errorf("answeredWhileBusy(%q) = %v, want %v", payload, got, want)
		}
	}
}
