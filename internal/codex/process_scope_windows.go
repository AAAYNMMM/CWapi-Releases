//go:build windows

package codex

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processScope struct {
	mu     sync.Mutex
	job    windows.Handle
	closed bool
}

func attachProcessScope(pid int) (*processScope, error) {
	if pid <= 0 {
		return nil, errors.New("CODEX_PROCESS_SCOPE_PID_INVALID")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CODEX_JOB_CREATE_FAILED: %w", err)
	}
	closeJob := true
	defer func() {
		if closeJob {
			_ = windows.CloseHandle(job)
		}
	}()

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if ret, setErr := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); ret == 0 {
		if setErr == nil {
			setErr = errors.New("SetInformationJobObject returned zero")
		}
		return nil, fmt.Errorf("CODEX_JOB_LIMITS_FAILED: %w", setErr)
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return nil, fmt.Errorf("CODEX_JOB_PROCESS_OPEN_FAILED: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return nil, fmt.Errorf("CODEX_JOB_ASSIGN_FAILED: %w", err)
	}

	closeJob = false
	return &processScope{job: job}, nil
}

func (s *processScope) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.job == 0 {
		return nil
	}
	err := windows.CloseHandle(s.job)
	s.job = 0
	if err != nil {
		return fmt.Errorf("CODEX_JOB_CLOSE_FAILED: %w", err)
	}
	return nil
}
