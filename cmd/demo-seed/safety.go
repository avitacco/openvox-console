package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// lookupFunc resolves a hostname to IP addresses. Injected so the checks
// below can be tested without DNS.
type lookupFunc func(host string) ([]net.IP, error)

// checkTarget reports whether rawURL names something this tool is allowed
// to write to, and explains itself when it does not.
//
// The seed writes a fabricated fleet - invented nodes, invented reports,
// invented job history. Against a real deployment that is not "demo data",
// it is corruption of that deployment's inventory, and openvoxdb has no
// undo. So the bar is deliberately high: the target must resolve entirely
// to loopback or private addresses, and the operator must have said out
// loud that they know what this is.
//
// Every address a name resolves to is checked, not just the first. A name
// that resolves to both 127.0.0.1 and a routable address is exactly the
// shape a split-horizon DNS entry for a real host takes, and picking the
// first answer would let it through half the time.
func checkTarget(what, rawURL string, confirmed bool, lookup lookupFunc) error {
	if !confirmed {
		return fmt.Errorf(
			"refusing to seed %s %s: this tool writes a fabricated fleet and is for a throwaway local stack only.\n"+
				"Pass --i-know-this-is-a-demo-console if that is what %s is.",
			what, rawURL, rawURL)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("refusing to seed %s: %q is not a usable URL: %w", what, rawURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("refusing to seed %s: %q has no host", what, rawURL)
	}

	// A literal address needs no resolver at all, whichever one is
	// injected - and asking a resolver to look up "127.0.0.1" is how you
	// get a refusal for the most obviously local target there is.
	ips := []net.IP{}
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	}

	if len(ips) == 0 {
		ips, err = lookup(host)
	}
	if err != nil {
		return fmt.Errorf("refusing to seed %s %s: cannot resolve %q to check it is local: %w", what, rawURL, host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("refusing to seed %s %s: %q resolved to no addresses", what, rawURL, host)
	}

	for _, ip := range ips {
		if !isLocalAddr(ip) {
			return fmt.Errorf(
				"refusing to seed %s %s: %q resolves to %s, which is not a loopback or private address.\n"+
					"This tool writes fabricated fleet data and must never run against a real deployment.",
				what, rawURL, host, ip)
		}
	}
	return nil
}

// isLocalAddr reports whether ip is one this tool considers part of a
// development stack: loopback, link-local, or RFC1918/ULA private space.
func isLocalAddr(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified()
}

// defaultLookup is the real resolver, with a small accommodation for
// container-network names: a bare hostname carrying no dots (compose
// service names like "console" or "openvoxdb") only resolves at all from
// inside that network, so failing to resolve one is not evidence it is
// remote. Anything with a dot in it gets no such benefit of the doubt.
func defaultLookup(host string) ([]net.IP, error) {
	ips, err := net.LookupIP(host)
	if err != nil && !strings.Contains(host, ".") {
		return nil, fmt.Errorf("%w (a bare name like this resolves only inside the compose network - run the seed from there, or point it at a published port on localhost)", err)
	}
	return ips, err
}
