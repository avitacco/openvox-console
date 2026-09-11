package certstatus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatuses_ParsesRealisticResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/puppet-ca/v1/certificate_statuses/all" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// A trimmed but structurally real response - see design.md,
		// verified live against a real openvoxserver.
		fmt.Fprint(w, `[
			{"name": "openvoxserver", "state": "signed", "fingerprint": "AA:BB"},
			{"name": "web01.example.com", "state": "requested", "fingerprint": "CC:DD"},
			{"name": "revoked-node.example.com", "state": "revoked", "fingerprint": "EE:FF"}
		]`)
	}))
	defer srv.Close()

	client := newClient(srv.URL, srv.Client())
	statuses, err := client.Statuses(context.Background())
	if err != nil {
		t.Fatalf("Statuses() error: %v", err)
	}

	want := map[string]string{
		"openvoxserver":            "signed",
		"web01.example.com":        "requested",
		"revoked-node.example.com": "revoked",
	}
	if len(statuses) != len(want) {
		t.Fatalf("Statuses() = %v, want %v", statuses, want)
	}
	for certname, wantState := range want {
		if got := statuses[certname]; got != wantState {
			t.Errorf("Statuses()[%q] = %q, want %q", certname, got, wantState)
		}
	}
}

func TestStatuses_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := newClient(srv.URL, srv.Client())
	if _, err := client.Statuses(context.Background()); err == nil {
		t.Fatal("Statuses() error = nil, want an error for a 403 response")
	}
}

// fakeCA models just enough of Puppet Server's per-node CA API
// (GET/PUT/DELETE certificate_status/:certname) to test Sign/Revoke/Clean
// against realistic request/response shapes.
type fakeCA struct {
	states map[string]string // certname -> state; absent key = no record
}

func newFakeCAServer(t *testing.T, ca *fakeCA) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/puppet-ca/v1/certificate_status/", func(w http.ResponseWriter, r *http.Request) {
		certname := r.URL.Path[len("/puppet-ca/v1/certificate_status/"):]
		switch r.Method {
		case http.MethodGet:
			state, ok := ca.states[certname]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name": %q, "state": %q}`, certname, state)
		case http.MethodPut:
			if _, ok := ca.states[certname]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var body struct {
				DesiredState string `json:"desired_state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			ca.states[certname] = body.DesiredState
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if _, ok := ca.states[certname]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(ca.states, certname)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSign_ApprovesPendingRequest(t *testing.T) {
	ca := &fakeCA{states: map[string]string{"web01.example.com": "requested"}}
	client := newClient(newFakeCAServer(t, ca).URL, http.DefaultClient)

	if err := client.Sign(context.Background(), "web01.example.com"); err != nil {
		t.Fatalf("Sign() error: %v", err)
	}
	if got := ca.states["web01.example.com"]; got != "signed" {
		t.Errorf("state after Sign() = %q, want %q", got, "signed")
	}
}

func TestSign_RejectsWrongState(t *testing.T) {
	ca := &fakeCA{states: map[string]string{"web01.example.com": "signed"}}
	client := newClient(newFakeCAServer(t, ca).URL, http.DefaultClient)

	err := client.Sign(context.Background(), "web01.example.com")
	var wrongState *WrongStateError
	if !errors.As(err, &wrongState) {
		t.Fatalf("Sign() error = %v, want a *WrongStateError", err)
	}
	if wrongState.CurrentState != "signed" || wrongState.RequiredState != "requested" {
		t.Errorf("WrongStateError = %+v, want CurrentState signed, RequiredState requested", wrongState)
	}
}

func TestRevoke_RevokesSignedCert(t *testing.T) {
	ca := &fakeCA{states: map[string]string{"web01.example.com": "signed"}}
	client := newClient(newFakeCAServer(t, ca).URL, http.DefaultClient)

	if err := client.Revoke(context.Background(), "web01.example.com"); err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}
	if got := ca.states["web01.example.com"]; got != "revoked" {
		t.Errorf("state after Revoke() = %q, want %q", got, "revoked")
	}
}

func TestRevoke_RejectsWrongState(t *testing.T) {
	ca := &fakeCA{states: map[string]string{"web01.example.com": "requested"}}
	client := newClient(newFakeCAServer(t, ca).URL, http.DefaultClient)

	err := client.Revoke(context.Background(), "web01.example.com")
	var wrongState *WrongStateError
	if !errors.As(err, &wrongState) {
		t.Fatalf("Revoke() error = %v, want a *WrongStateError", err)
	}
	if wrongState.CurrentState != "requested" || wrongState.RequiredState != "signed" {
		t.Errorf("WrongStateError = %+v, want CurrentState requested, RequiredState signed", wrongState)
	}
}

func TestClean_RemovesCertRecord(t *testing.T) {
	ca := &fakeCA{states: map[string]string{"web01.example.com": "revoked"}}
	client := newClient(newFakeCAServer(t, ca).URL, http.DefaultClient)

	if err := client.Clean(context.Background(), "web01.example.com"); err != nil {
		t.Fatalf("Clean() error: %v", err)
	}
	if _, ok := ca.states["web01.example.com"]; ok {
		t.Error("cert record still present after Clean()")
	}
}

func TestClean_UnknownCertnameIsNotFoundError(t *testing.T) {
	ca := &fakeCA{states: map[string]string{}}
	client := newClient(newFakeCAServer(t, ca).URL, http.DefaultClient)

	err := client.Clean(context.Background(), "never-existed.example.com")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Clean() error = %v, want a *NotFoundError", err)
	}
}
