package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// staticLookup returns fixed answers, so these tests never touch DNS.
func staticLookup(answers map[string][]string) lookupFunc {
	return func(host string) ([]net.IP, error) {
		addrs, ok := answers[host]
		if !ok {
			return nil, fmt.Errorf("no such host: %s", host)
		}
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ips = append(ips, net.ParseIP(a))
		}
		return ips, nil
	}
}

func TestCheckTargetRequiresConfirmation(t *testing.T) {
	lookup := staticLookup(map[string][]string{"localhost": {"127.0.0.1"}})

	err := checkTarget("console", "http://localhost:8080", false, lookup)
	if err == nil {
		t.Fatal("expected refusal without the confirmation flag, got nil")
	}
	// The refusal has to name the target and say what to do about it,
	// or the operator is left guessing which of two URLs was rejected.
	if !strings.Contains(err.Error(), "http://localhost:8080") {
		t.Errorf("refusal does not name the target: %v", err)
	}
	if !strings.Contains(err.Error(), "--i-know-this-is-a-demo-console") {
		t.Errorf("refusal does not say how to proceed: %v", err)
	}
}

func TestCheckTargetAcceptsLocalTargets(t *testing.T) {
	lookup := staticLookup(map[string][]string{
		"localhost": {"127.0.0.1"},
		"console":   {"172.18.0.4"},
		"dev-box":   {"192.168.1.20"},
	})

	for _, target := range []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"https://console:8080",
		"http://dev-box:8080",
		"https://[::1]:8081",
	} {
		if err := checkTarget("console", target, true, lookup); err != nil {
			t.Errorf("checkTarget(%q) = %v, want nil", target, err)
		}
	}
}

func TestCheckTargetRefusesRemoteTargets(t *testing.T) {
	lookup := staticLookup(map[string][]string{
		"console.example.com": {"93.184.216.34"},
		// Split-horizon shape: one local answer, one routable. Checking
		// only the first address would let this through.
		"console.internal.example.com": {"127.0.0.1", "93.184.216.34"},
	})

	for _, target := range []string{
		"https://console.example.com",
		"https://console.internal.example.com",
	} {
		err := checkTarget("console", target, true, lookup)
		if err == nil {
			t.Errorf("checkTarget(%q) = nil, want a refusal", target)
			continue
		}
		if !strings.Contains(err.Error(), "93.184.216.34") {
			t.Errorf("refusal for %q does not name the offending address: %v", target, err)
		}
	}
}

func TestCheckTargetRefusesUnresolvableHost(t *testing.T) {
	lookup := staticLookup(map[string][]string{})

	err := checkTarget("openvoxdb", "https://nowhere.example.com:8081", true, lookup)
	if err == nil {
		t.Fatal("expected refusal for an unresolvable host, got nil")
	}
	if !strings.Contains(err.Error(), "nowhere.example.com") {
		t.Errorf("refusal does not name the host: %v", err)
	}
}
