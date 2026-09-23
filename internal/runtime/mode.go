package runtime

import (
	"fmt"
	"strings"
)

// Mode is the role a console instance runs as. One binary and one
// container image serve every mode; which one is active is configuration
// (CONSOLE_RUN_MODE), not a separate build, entrypoint, or subcommand -
// see the run-modes capability.
//
// The point of a mode is that components with very different load
// profiles can be scaled and placed independently: the ENC endpoint
// answers a request per node per run interval, while the background
// schedulers run a handful of syncs a day.
type Mode string

const (
	// ModeAll activates every surface. It is the default, and is
	// deliberately identical to the binary's behavior before run modes
	// existed, so an existing deployment upgrades without configuration
	// changes and without opting into anything.
	ModeAll Mode = "all"

	// ModeWeb serves the console UI and the REST API, including code
	// deployment endpoints and webhooks. It terminates no node
	// connections and runs no background workers.
	ModeWeb Mode = "web"

	// ModeENC serves only the ENC classification endpoint, plus the
	// operational endpoints every mode serves. It is the mode intended
	// to run alongside a compiler, so classification is a local call
	// rather than a trip back to the console core.
	ModeENC Mode = "enc"

	// ModeOrchestrator terminates node transport connections and
	// dispatches orchestration jobs. It serves neither the UI nor the
	// REST API.
	ModeOrchestrator Mode = "orchestrator"

	// ModeWorker runs scheduled background work. It serves neither the
	// UI nor the REST API and terminates no node connections.
	ModeWorker Mode = "worker"
)

// modes is the authoritative list, in the order error messages and
// documentation present them: the default first, then the rest in the
// order an operator would typically split them out.
var modes = []Mode{ModeAll, ModeWeb, ModeENC, ModeOrchestrator, ModeWorker}

// Modes returns every supported mode, in presentation order.
func Modes() []Mode {
	out := make([]Mode, len(modes))
	copy(out, modes)
	return out
}

// String returns the mode's configuration spelling.
func (m Mode) String() string { return string(m) }

// ParseMode maps a CONSOLE_RUN_MODE value to a Mode. An empty value is
// not an error: it means the setting was never set, which must select
// ModeAll so that a deployment predating run modes keeps working
// untouched.
//
// An unrecognized value is refused rather than defaulted. Defaulting a
// typo ("wrker") to ModeAll would start an instance serving everything
// while its operator believed it served background work only - a silent
// mismatch between intent and behavior that is much harder to notice
// than a refused startup.
func ParseMode(s string) (Mode, error) {
	if s == "" {
		return ModeAll, nil
	}
	for _, m := range modes {
		if s == string(m) {
			return m, nil
		}
	}
	return "", &InvalidModeError{Value: s}
}

// InvalidModeError reports an unrecognized run mode string.
type InvalidModeError struct {
	Value string
}

func (e *InvalidModeError) Error() string {
	names := make([]string, len(modes))
	for i, m := range modes {
		names[i] = fmt.Sprintf("%q", m)
	}
	return fmt.Sprintf("invalid run mode %q: must be one of %s", e.Value, strings.Join(names, ", "))
}

// DefaultSigningKeyID is the kid assumed when CONSOLE_RBAC_SIGNING_KEY_ID
// is unset, so that an existing single-key deployment's already-issued
// tokens (which carry no kid) keep verifying unchanged.
const DefaultSigningKeyID = "default"

// IssuesTokens reports whether this mode can mint access tokens, and so
// needs the RBAC signing key.
//
// Only modes serving the login and refresh endpoints issue anything;
// every other mode merely *verifies* presented tokens, which needs public
// keys alone. Keeping the private key off those instances is the point -
// an enc instance typically runs next to a compiler, outside the
// console's own trust boundary, and has no business being able to mint a
// token for anyone.
func (m Mode) IssuesTokens() bool {
	return m == ModeAll || m == ModeWeb
}
