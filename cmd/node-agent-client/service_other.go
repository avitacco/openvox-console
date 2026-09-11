//go:build !windows

package main

import "log/slog"

// runAsWindowsService always returns false on non-Windows platforms -
// there's no Service Control Manager to check in with, so main's normal
// signal-driven path always runs instead.
func runAsWindowsService(logger *slog.Logger) bool {
	return false
}
