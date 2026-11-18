// Package classcatalog knows which classes each code environment
// defines, with their parameters, types and defaults - see design.md in
// add-class-discovery-and-run-options.
//
// The classes come from openvoxserver (/puppet/v3/environment_classes),
// which parses the code with Puppet's own parser; nothing here reads a
// manifest. Answers are kept in memory per console instance: they are
// derived data the server can always reproduce, so there is nothing to
// store or to keep in step between instances.
//
// Nothing on the classification path calls this package. What a node is
// classified with never depends on what is found here, or on whether
// anything can be found at all.
package classcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// Freshness is how long an environment's classes are reused before they
// are read again. A deploy or a requested refresh cuts it short.
const Freshness = 5 * time.Minute

// failureMemo is how long a failed read is remembered, so that a server
// that is down delays one request in this window rather than every one.
const failureMemo = 30 * time.Second

// readTimeout bounds one read of the server. Without its class cache
// the server parses every manifest per request, which takes seconds on
// a large environment - but not this long.
const readTimeout = 30 * time.Second

// Parameter is one parameter a class declares.
type Parameter struct {
	Name string `json:"name"`
	// Type is the declared Puppet type, or "" when none is declared.
	Type string `json:"type,omitempty"`
	// Required is true when the class gives the parameter no default.
	Required bool `json:"required"`
	// DefaultSource is the default as written in the manifest.
	DefaultSource string `json:"defaultSource,omitempty"`
	// Default is the default's value, present only when it is a literal
	// rather than an expression.
	Default json.RawMessage `json:"default,omitempty"`
}

// Class is one class an environment's code defines.
type Class struct {
	Name       string      `json:"name"`
	Parameters []Parameter `json:"parameters"`
}

// FileError is a manifest that could not be parsed. Whatever classes it
// defines are unknown.
type FileError struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// Environment is the classes of one environment as last read.
type Environment struct {
	Environment string    `json:"environment"`
	FetchedAt   time.Time `json:"fetchedAt"`
	// Stale is true when the server could not be read just now and this
	// is an earlier answer.
	Stale   bool        `json:"stale"`
	Classes []Class     `json:"classes"`
	Errors  []FileError `json:"errors"`
}

// Class returns the named class, if the environment defines it.
func (e Environment) Class(name string) (Class, bool) {
	i := sort.Search(len(e.Classes), func(i int) bool { return e.Classes[i].Name >= name })
	if i < len(e.Classes) && e.Classes[i].Name == name {
		return e.Classes[i], true
	}
	return Class{}, false
}

// Reasons discovery can be unavailable, as the API reports them.
const (
	ReasonNotConfigured = "not_configured"
	ReasonForbidden     = "forbidden"
	ReasonUnreachable   = "unreachable"
)

// Reason classifies err as one of the Reason* constants, or "" when it
// is something else (an environment that does not exist, say).
func Reason(err error) string {
	var fe *taskcatalog.ForbiddenError
	var ue *taskcatalog.UnreachableError
	switch {
	case errors.Is(err, taskcatalog.ErrNotConfigured):
		return ReasonNotConfigured
	case errors.As(err, &fe):
		return ReasonForbidden
	case errors.As(err, &ue), errors.Is(err, context.DeadlineExceeded):
		return ReasonUnreachable
	}
	return ""
}

// Source is the part of *taskcatalog.Client this package reads. A nil
// *taskcatalog.Client satisfies it and reports ErrNotConfigured.
type Source interface {
	Environments(ctx context.Context) ([]string, error)
	EnvironmentClasses(ctx context.Context, environment, etag string) (*taskcatalog.EnvironmentClasses, bool, error)
}

type entry struct {
	env     Environment
	etag    string
	expires time.Time
	// failed and failedAt remember the last read's failure, when there
	// is no earlier answer to serve instead.
	failed   error
	failedAt time.Time
	hasEnv   bool
}

type call struct {
	done chan struct{}
	env  Environment
	err  error
}

// Catalog serves each environment's classes, reading the server only
// when what it holds is older than Freshness. The zero value is not
// usable; see New.
type Catalog struct {
	src Source
	now func() time.Time

	mu       sync.Mutex
	entries  map[string]*entry
	inflight map[string]*call
}

// New builds a Catalog reading from src.
func New(src Source) *Catalog {
	return &Catalog{src: src, now: time.Now, entries: map[string]*entry{}, inflight: map[string]*call{}}
}

// Environments lists the environments the server has, sorted.
func (c *Catalog) Environments(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return c.src.Environments(ctx)
}

// Classes returns environment's classes, from memory when they are
// fresh. When the server cannot be reached and there is an earlier
// answer, that answer is returned marked stale.
func (c *Catalog) Classes(ctx context.Context, environment string) (Environment, error) {
	return c.get(ctx, environment, false)
}

// Refresh reads environment's classes from the server now, whatever is
// held.
func (c *Catalog) Refresh(ctx context.Context, environment string) (Environment, error) {
	return c.get(ctx, environment, true)
}

// ExpireAll makes the next request for every environment read the
// server again. What is held stays, to be served stale if that read
// fails.
func (c *Catalog) ExpireAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		e.expires = time.Time{}
		e.failed = nil
	}
}

func (c *Catalog) get(ctx context.Context, environment string, force bool) (Environment, error) {
	c.mu.Lock()
	now := c.now()
	if e := c.entries[environment]; e != nil && !force {
		if e.hasEnv && now.Before(e.expires) {
			env := e.env
			c.mu.Unlock()
			return env, nil
		}
		if e.failed != nil && now.Sub(e.failedAt) < failureMemo {
			env, hasEnv, err := e.env, e.hasEnv, e.failed
			c.mu.Unlock()
			if hasEnv {
				env.Stale = true
				return env, nil
			}
			return Environment{}, err
		}
	}
	// One read per environment at a time: whoever arrives while one is
	// under way waits for it and shares its answer.
	if running := c.inflight[environment]; running != nil {
		c.mu.Unlock()
		select {
		case <-running.done:
			return running.env, running.err
		case <-ctx.Done():
			return Environment{}, ctx.Err()
		}
	}
	running := &call{done: make(chan struct{})}
	c.inflight[environment] = running
	c.mu.Unlock()

	// Detached from the caller: others may be waiting on this read, and
	// its answer is worth keeping even if the first asker has gone.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readTimeout)
	running.env, running.err = c.read(readCtx, environment)
	cancel()

	c.mu.Lock()
	delete(c.inflight, environment)
	c.mu.Unlock()
	close(running.done)
	return running.env, running.err
}

// read asks the server and updates the entry for environment.
func (c *Catalog) read(ctx context.Context, environment string) (Environment, error) {
	c.mu.Lock()
	held := c.entries[environment]
	etag := ""
	if held != nil && held.hasEnv {
		etag = held.etag
	}
	c.mu.Unlock()

	resp, notModified, err := c.src.EnvironmentClasses(ctx, environment, etag)

	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	switch {
	case err == nil && notModified && held != nil && held.hasEnv:
		held.env.FetchedAt = now
		held.expires = now.Add(Freshness)
		held.failed = nil
		return held.env, nil
	case err == nil && resp != nil:
		env := convert(environment, resp, now)
		c.entries[environment] = &entry{env: env, etag: resp.ETag, expires: now.Add(Freshness), hasEnv: true}
		return env, nil
	case err == nil:
		// "Not modified" with nothing held to be unmodified.
		err = &taskcatalog.UnreachableError{Err: errors.New("server answered not modified to an unconditional request")}
	}

	// Only a server that cannot be reached leaves the earlier answer
	// standing. One that refuses, or no longer has the environment, has
	// given an answer of its own, and the earlier one is dropped.
	if Reason(err) == ReasonUnreachable && held != nil && held.hasEnv {
		held.failed, held.failedAt = err, now
		env := held.env
		env.Stale = true
		return env, nil
	}
	c.entries[environment] = &entry{failed: err, failedAt: now}
	return Environment{}, err
}

// convert flattens the server's per-file answer into sorted classes and
// the files that failed to parse.
func convert(environment string, resp *taskcatalog.EnvironmentClasses, now time.Time) Environment {
	env := Environment{Environment: environment, FetchedAt: now, Classes: []Class{}, Errors: []FileError{}}
	for _, f := range resp.Files {
		if f.Error != "" {
			env.Errors = append(env.Errors, FileError{File: relativePath(f.Path, environment), Error: f.Error})
			continue
		}
		for _, cl := range f.Classes {
			class := Class{Name: cl.Name, Parameters: make([]Parameter, 0, len(cl.Params))}
			for _, p := range cl.Params {
				class.Parameters = append(class.Parameters, Parameter{
					Name:          p.Name,
					Type:          p.Type,
					Required:      !p.HasDefault,
					DefaultSource: p.DefaultSource,
					Default:       p.DefaultLiteral,
				})
			}
			env.Classes = append(env.Classes, class)
		}
	}
	sort.SliceStable(env.Classes, func(i, j int) bool { return env.Classes[i].Name < env.Classes[j].Name })
	sort.Slice(env.Errors, func(i, j int) bool { return env.Errors[i].File < env.Errors[j].File })
	return env
}

// relativePath shortens a manifest's path on the server to its path
// inside the environment, which is what its author recognises.
func relativePath(path, environment string) string {
	marker := "/" + environment + "/"
	if i := strings.Index(path, "/environments"+marker); i >= 0 {
		return path[i+len("/environments"+marker):]
	}
	if i := strings.LastIndex(path, marker); i >= 0 {
		return path[i+len(marker):]
	}
	return path
}
