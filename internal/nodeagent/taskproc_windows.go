//go:build windows

package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"github.com/voxpupuli/enterprise-console/internal/taskexec"
	"os"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunTaskProcess is the production TaskRunner. The task runs inside a
// job object, so a timeout terminates everything it started, not only
// the process the agent started (design decision 8 in add-task-execution).
func RunTaskProcess(ctx context.Context, c taskCommand) (stdout, stderr string, exitCode int, timedOut bool, err error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return "", "", 0, false, err
	}
	defer windows.CloseHandle(job)
	// Closing the job's last handle kills whatever is still in it, so
	// nothing outlives the agent's interest in it either.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return "", "", 0, false, err
	}

	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdin = bytes.NewReader(c.Stdin)
	outBuf := taskexec.NewCapped(maxTaskStdout)
	errBuf := taskexec.NewCapped(maxTaskStderr)
	cmd.Stdout, cmd.Stderr = outBuf, errBuf
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Start(); err != nil {
		return "", "", 0, false, err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, proc)
		windows.CloseHandle(proc)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", "", 0, false, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(c.Timeout)
	defer timer.Stop()
	var runErr error
	select {
	case runErr = <-done:
	case <-timer.C:
		timedOut = true
		_ = windows.TerminateJobObject(job, 1)
		runErr = <-done
	case <-ctx.Done():
		_ = windows.TerminateJobObject(job, 1)
		runErr = <-done
	}

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
