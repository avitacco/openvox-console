//go:build !linux

package codemanager

// exchangePaths has no portable equivalent outside Linux, so every
// caller falls back to a plain rename. The console itself is deployed on
// Linux (see the Dockerfile and SETUP.md); this exists so the package
// still builds and tests on a developer's macOS or Windows machine.
func exchangePaths(a, b string) error {
	return errExchangeUnsupported
}
