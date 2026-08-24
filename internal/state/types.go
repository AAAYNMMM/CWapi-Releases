package state

// ExecutionEventRecord is a structured execution lifecycle event.
type ExecutionEventRecord struct {
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

// RuntimeLogRecord is the persistent CWapi runtime log surface.
type RuntimeLogRecord struct {
	ID          int64  `json:"id"`
	Timestamp   int64  `json:"timestamp"`
	Level       string `json:"level"`
	Component   string `json:"component"`
	Message     string `json:"message"`
	FieldsJSON  string `json:"fields_json"`
	Fingerprint string `json:"fingerprint"`
}
