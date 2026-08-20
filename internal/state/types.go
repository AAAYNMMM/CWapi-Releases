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

// ErrorAggregateRecord deduplicates a persistent error into one active item
// with an occurrence count rather than a new GUI popup every refresh cycle.
type ErrorAggregateRecord struct {
	Fingerprint string `json:"fingerprint"`
	Component   string `json:"component"`
	Operation   string `json:"operation"`
	Message     string `json:"message"`
	Count       int64  `json:"count"`
	FirstSeen   int64  `json:"first_seen"`
	LastSeen    int64  `json:"last_seen"`
	Active      bool   `json:"active"`
}
