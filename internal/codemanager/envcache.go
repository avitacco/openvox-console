package codemanager

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// EnvironmentCacheFlush makes every Puppet server that may hold a cached
// copy of an environment reload it after a deploy. With
// environment_timeout = unlimited a server otherwise keeps compiling the
// code it loaded before the deploy until it restarts.
//
// The servers are the one the console is configured with (Primary) plus
// every server openvoxdb says compiled a catalog recently (Servers). A
// server that has compiled nothing within that window has either never
// loaded the environment or is not serving nodes, so missing it does no
// harm worth a manual server list.
type EnvironmentCacheFlush struct {
	// Primary is CONSOLE_PUPPET_SERVER_URL. Always flushed.
	Primary string
	// Servers returns the certnames of the servers that compiled recent
	// catalogs. Nil flushes Primary only.
	Servers func(ctx context.Context) ([]string, error)
	// Addresses maps a certname to the URL it is reached at, for a
	// server not reachable at https://<certname>:8140.
	Addresses map[string]string
	// Flush drops environment from the cache of the server at baseURL.
	Flush func(ctx context.Context, baseURL, environment string) error
}

// SetEnvironmentCacheFlush wires the post-deploy flush. Without it no
// server is told about new code.
func (h *Handlers) SetEnvironmentCacheFlush(f *EnvironmentCacheFlush) { h.cacheFlush = f }

const (
	// environmentCacheFlushTimeout bounds each server's flush, so a
	// server that does not answer cannot hold the deploy (and its lease)
	// open. The flushes run in parallel, so this is also the total.
	environmentCacheFlushTimeout = 30 * time.Second
	// serverDiscoveryTimeout bounds the openvoxdb lookup.
	serverDiscoveryTimeout = 10 * time.Second
	// defaultPuppetServerPort is where a discovered server is reached
	// when Addresses does not name it.
	defaultPuppetServerPort = "8140"
)

// flushTarget is one server to flush: its URL, and a name for messages.
type flushTarget struct {
	name, url string
}

// targets returns the servers to flush, Primary first, each server
// once. A discovered server whose address is Primary's is not repeated.
// The error reports a failed discovery; Primary is returned regardless.
func (f *EnvironmentCacheFlush) targets(ctx context.Context) ([]flushTarget, error) {
	var out []flushTarget
	seen := map[string]bool{}
	add := func(name, u string) {
		key := hostKey(u)
		if u == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, flushTarget{name: name, url: strings.TrimRight(u, "/")})
	}
	add(f.Primary, f.Primary)
	if f.Servers == nil {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, serverDiscoveryTimeout)
	defer cancel()
	certnames, err := f.Servers(ctx)
	if err != nil {
		return out, err
	}
	sort.Strings(certnames)
	for _, certname := range certnames {
		if u, ok := f.Addresses[certname]; ok {
			add(certname, u)
			continue
		}
		add(certname, "https://"+net.JoinHostPort(certname, defaultPuppetServerPort))
	}
	return out, nil
}

// hostKey reduces a server URL to lower-case host:port, so the same
// server named twice - once configured, once discovered - is flushed
// once.
func hostKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(raw)
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(net.JoinHostPort(u.Hostname(), port))
}

// flushEnvironmentCache asks every Puppet server to reload
// environment's code after it has been activated. Each failure is
// logged and recorded as activity naming the server: the deploy itself
// stands, but nodes compiled by that server keep getting catalogs built
// from the previous code until it is flushed or restarted, and that must
// not pass unnoticed.
func (h *Handlers) flushEnvironmentCache(ctx context.Context, environment string, r *http.Request) {
	if h.cacheFlush == nil || h.cacheFlush.Flush == nil {
		return
	}
	targets, err := h.cacheFlush.targets(ctx)
	if err != nil {
		h.logger.Warn("could not look up which Puppet servers compile catalogs; flushing only the configured server",
			"error", err, "environment", environment)
		h.recordActivity(r, "deploy.cache_flush_failed",
			fmt.Sprintf("deployed %s, but could not look up which Puppet servers to tell about it, so only the configured server was told: %s", environment, err.Error()))
	}

	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, environmentCacheFlushTimeout)
			defer cancel()
			errs[i] = h.cacheFlush.Flush(ctx, t.url, environment)
		}()
	}
	wg.Wait()

	// Recorded here, in order, rather than from the goroutines.
	for i, t := range targets {
		if errs[i] == nil {
			continue
		}
		h.logger.Warn("deployed code is on disk but a Puppet server's environment cache was not flushed; it keeps compiling the previous code until flushed or restarted",
			"error", errs[i], "environment", environment, "server", t.name, "url", t.url)
		h.recordActivity(r, "deploy.cache_flush_failed",
			fmt.Sprintf("deployed %s, but Puppet server %s was not told to reload it: %s", environment, t.name, errs[i].Error()))
	}
}
