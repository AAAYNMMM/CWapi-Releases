package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/processruntime"
)

const desktopProcessLimit = 12

type ProcessFailureSnapshot struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProcessSnapshot struct {
	ProcessID        string                  `json:"process_id"`
	State            string                  `json:"state"`
	Backend          string                  `json:"backend"`
	Repository       string                  `json:"repository"`
	ExpectedCommit   string                  `json:"expected_commit"`
	WorkingDirectory string                  `json:"working_directory"`
	StartedAt        int64                   `json:"started_at"`
	UpdatedAt        int64                   `json:"updated_at"`
	ExitCode         *int                    `json:"exit_code,omitempty"`
	StdoutTail       string                  `json:"stdout_tail"`
	StderrTail       string                  `json:"stderr_tail"`
	LatestStream     string                  `json:"latest_stream"`
	LatestOutputAt   int64                   `json:"latest_output_at"`
	Error            *ProcessFailureSnapshot `json:"error,omitempty"`
}

type DesktopSnapshot struct {
	GeneratedAt        int64                   `json:"generated_at"`
	Runtime            RuntimeSnapshot         `json:"runtime"`
	Config             ConfigSnapshot          `json:"config"`
	Slack              SlackSnapshot           `json:"slack"`
	Codex              CodexSnapshot           `json:"codex"`
	Processes          []ProcessSnapshot       `json:"processes"`
	LatestExecution    *ExecutionEventSnapshot `json:"latest_execution,omitempty"`
	LatestRuntimeError *RuntimeLogSnapshot     `json:"latest_runtime_error,omitempty"`
	Components         []ComponentSnapshot     `json:"components"`
}

func (s *Service) DesktopSnapshot(ctx context.Context, limit int) (DesktopSnapshot, error) {
	if s == nil || s.state == nil {
		return DesktopSnapshot{}, fmt.Errorf("DESKTOP_STATE_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.state.Ping(ctx); err != nil {
		s.recordOperationalError("desktop", "state.ping", err)
		return DesktopSnapshot{}, err
	}
	if limit < 1 || limit > desktopProcessLimit {
		limit = desktopProcessLimit
	}
	observability := s.ObservabilitySnapshot()
	var latestExecution *ExecutionEventSnapshot
	if count := len(observability.StructuredExecution); count > 0 {
		latest := observability.StructuredExecution[count-1]
		latestExecution = &latest
	}
	var latestRuntimeError *RuntimeLogSnapshot
	for index := len(observability.RuntimeLogs) - 1; index >= 0; index-- {
		candidate := observability.RuntimeLogs[index]
		if candidate.Level == "error" || candidate.Level == "fatal" {
			latestRuntimeError = &candidate
			break
		}
	}
	processes := []ProcessSnapshot{}
	if s.processRuntime != nil {
		processes = processSnapshots(s.processRuntime.Records(limit))
	}
	return DesktopSnapshot{
		GeneratedAt: time.Now().UnixMilli(), Runtime: s.RuntimeSnapshot(),
		Config: s.ConfigSnapshot(), Slack: s.SlackSnapshot(), Codex: s.CodexSnapshot(),
		Processes: processes, LatestExecution: latestExecution,
		LatestRuntimeError: latestRuntimeError, Components: observability.Components,
	}, nil
}

func (s *Service) StopProcess(processID string) (ProcessSnapshot, error) {
	if s == nil || s.processRuntime == nil {
		return ProcessSnapshot{}, fmt.Errorf("PROCESS_RUNTIME_UNAVAILABLE")
	}
	record, err := s.processRuntime.StopRecord(strings.TrimSpace(processID))
	if err != nil {
		s.recordOperationalError("process", "process.stop.desktop", err)
		return ProcessSnapshot{}, err
	}
	return processSnapshot(record), nil
}

func processSnapshots(records []processruntime.Record) []ProcessSnapshot {
	result := make([]ProcessSnapshot, len(records))
	for index, record := range records {
		result[index] = processSnapshot(record)
	}
	return result
}

func processSnapshot(record processruntime.Record) ProcessSnapshot {
	result := ProcessSnapshot{
		ProcessID: record.ProcessID, State: record.State, Backend: record.Backend,
		Repository: record.Repository, ExpectedCommit: record.ExpectedCommit,
		WorkingDirectory: record.WorkingDirectory, StartedAt: record.StartedAt,
		UpdatedAt: record.UpdatedAt, ExitCode: record.ExitCode,
		StdoutTail: record.StdoutTail, StderrTail: record.StderrTail,
		LatestStream: record.LatestStream, LatestOutputAt: record.LatestOutputAt,
	}
	if record.Error != nil {
		result.Error = &ProcessFailureSnapshot{Code: record.Error.Code, Message: record.Error.Message}
	}
	return result
}
