//go:build windows

package main

import (
	"context"
	"log/slog"

	"golang.org/x/sys/windows/svc"
)

// runAsWindowsService runs node-agent-client under the Service Control
// Manager's supervision when launched by it (returning true - the
// caller must not also try the normal foreground path), or returns
// false when run interactively (e.g. manual testing at a console),
// falling through to main's normal signal-driven path instead.
func runAsWindowsService(logger *slog.Logger) bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}

	if err := svc.Run("node-agent-client", &windowsService{logger: logger}); err != nil {
		logger.Error("windows service exited", "error", err)
	}
	return true
}

// windowsService adapts run's context-cancellation lifecycle to the
// SCM's Start/Stop/Shutdown protocol - the only translation needed,
// since run itself already knows nothing about how it gets cancelled.
type windowsService struct {
	logger *slog.Logger
}

func (s *windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, s.logger) }()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			if err != nil {
				s.logger.Error("node-agent-client exited", "error", err)
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
