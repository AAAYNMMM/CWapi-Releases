package app

import (
	"context"

	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type ExecutionEventSnapshot struct {
	ID         int64  `json:"id"`
	Timestamp  int64  `json:"timestamp"`
	TaskID     string `json:"task_id"`
	StepID     string `json:"step_id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
	DataJSON   string `json:"data_json"`
}

type RuntimeLogSnapshot struct {
	ID          int64  `json:"id"`
	Timestamp   int64  `json:"timestamp"`
	Level       string `json:"level"`
	Component   string `json:"component"`
	Message     string `json:"message"`
	FieldsJSON  string `json:"fields_json"`
	Fingerprint string `json:"fingerprint"`
}

type ErrorSnapshot struct {
	Fingerprint string `json:"fingerprint"`
	Component   string `json:"component"`
	Operation   string `json:"operation"`
	Message     string `json:"message"`
	Count       int64  `json:"count"`
	FirstSeen   int64  `json:"first_seen"`
	LastSeen    int64  `json:"last_seen"`
	Active      bool   `json:"active"`
}

type ComponentSnapshot struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Detail    string `json:"detail"`
	UpdatedAt int64  `json:"updated_at"`
}

type ObservabilitySnapshot struct {
	StatePath           string                   `json:"state_path"`
	StateSchema         string                   `json:"state_schema"`
	StructuredExecution []ExecutionEventSnapshot `json:"structured_execution"`
	RuntimeLogs         []RuntimeLogSnapshot     `json:"runtime_logs"`
	Errors              []ErrorSnapshot          `json:"errors"`
	Components          []ComponentSnapshot      `json:"components"`
}

func (s *Service) ObservabilitySnapshot() ObservabilitySnapshot {
	return observabilitySnapshot(s.observability.Snapshot())
}

func observabilitySnapshot(source observability.Snapshot) ObservabilitySnapshot {
	execution := make([]ExecutionEventSnapshot, len(source.StructuredExecution))
	for index, record := range source.StructuredExecution {
		execution[index] = executionEventSnapshot(record)
	}
	runtimeLogs := make([]RuntimeLogSnapshot, len(source.RuntimeLogs))
	for index, record := range source.RuntimeLogs {
		runtimeLogs[index] = runtimeLogSnapshot(record)
	}
	errors := make([]ErrorSnapshot, len(source.Errors))
	for index, record := range source.Errors {
		errors[index] = errorSnapshot(record)
	}
	components := make([]ComponentSnapshot, len(source.Components))
	for index, record := range source.Components {
		components[index] = ComponentSnapshot{
			Name:      record.Name,
			State:     record.State,
			Detail:    record.Detail,
			UpdatedAt: record.UpdatedAt,
		}
	}
	return ObservabilitySnapshot{
		StatePath:           source.StatePath,
		StateSchema:         source.StateSchema,
		StructuredExecution: execution,
		RuntimeLogs:         runtimeLogs,
		Errors:              errors,
		Components:          components,
	}
}

func executionEventSnapshot(record state.ExecutionEventRecord) ExecutionEventSnapshot {
	return ExecutionEventSnapshot{
		ID:         record.ID,
		Timestamp:  record.Timestamp,
		TaskID:     record.TaskID,
		StepID:     record.StepID,
		Kind:       record.Kind,
		Status:     record.Status,
		Message:    record.Message,
		DurationMS: record.DurationMS,
		DataJSON:   record.DataJSON,
	}
}

func runtimeLogSnapshot(record state.RuntimeLogRecord) RuntimeLogSnapshot {
	return RuntimeLogSnapshot{
		ID:          record.ID,
		Timestamp:   record.Timestamp,
		Level:       record.Level,
		Component:   record.Component,
		Message:     record.Message,
		FieldsJSON:  record.FieldsJSON,
		Fingerprint: record.Fingerprint,
	}
}

func errorSnapshot(record state.ErrorAggregateRecord) ErrorSnapshot {
	return ErrorSnapshot{
		Fingerprint: record.Fingerprint,
		Component:   record.Component,
		Operation:   record.Operation,
		Message:     record.Message,
		Count:       record.Count,
		FirstSeen:   record.FirstSeen,
		LastSeen:    record.LastSeen,
		Active:      record.Active,
	}
}

func (s *Service) runtimeInfo(component, message string, fields map[string]any) {
	_, _ = s.observability.LogRuntime(context.Background(), observability.RuntimeInput{
		Level:     "info",
		Component: component,
		Message:   message,
		Fields:    fields,
	})
}

func (s *Service) recordOperationalError(component, operation string, err error) {
	if err == nil {
		return
	}
	_, _ = s.observability.RecordError(context.Background(), observability.ErrorInput{
		Component: component,
		Operation: operation,
		Message:   err.Error(),
	})
}
