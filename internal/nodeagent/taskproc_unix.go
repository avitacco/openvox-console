//go:build !windows

package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"github.com/voxpupuli/enterprise-console/internal/taskexec"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// RunTaskProcess is the production TaskRunner. The task starts in a
// process group of its own, so a timeout kills everything it started -
// a backgrounded child included - not only the process the agent
// started (design decision 8 in add-task-execution).
func RunTaskProcess(ctx context.Context, c taskCommand) (stdout, stderr string, exitCode int, timedOut bool, err error) {
	runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdin = bytes.NewReader(c.Stdin)
	outBuf := taskexec.NewCapped(maxTaskStdout)
	errBuf := taskexec.NewCapped(maxTaskStderr)
	cmd.Stdout, cmd.Stderr = outBuf, errBuf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// A process the task left running in the background can hold its
	// output open after the task itself exits; stop waiting for it.
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()
	timedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	var exitErr *exec.ExitError
	switch {
	case runErr == nil, errors.Is(runErr, exec.ErrWaitDelay):
		return outBuf.String(), errBuf.String(), 0, timedOut, nil
	case errors.As(runErr, &exitErr):
		return outBuf.String(), errBuf.String(), exitErr.ExitCode(), timedOut, nil
	default:
		return outBuf.String(), errBuf.String(), 0, timedOut, runErr
	}
}
