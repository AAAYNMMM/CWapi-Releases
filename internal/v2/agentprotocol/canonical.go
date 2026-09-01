package agentprotocol

import "time"

const DefaultModel = "cwapi-web-gpt"

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Capabilities struct {
	Streaming     bool `json:"streaming"`
	Tools         bool `json:"tools"`
	ParallelTools bool `json:"parallel_tools"`
	Images        bool `json:"images"`
	Files         bool `json:"files"`
}

type Conversation struct {
	Model          string
	Messages       []Message
	Tools          []ToolDefinition
	ToolChoice     ToolChoice
	ResponseFormat ResponseFormat
	Metadata       map[string]any
	Stream         bool
}

type Message struct {
	Role       Role
	Content    string
	Name       string
	ToolCalls  []ToolCall
	ToolResult *ToolResult
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ToolResult struct {
	CallID  string
	Name    string
	Content string
}

type ToolChoice struct {
	Mode string
	Name string
}

type ResponseFormat struct {
	Type       string
	JSONSchema map[string]any
}

type CompletionStatus string

const (
	CompletionCompleted CompletionStatus = "completed"
	CompletionCanceled  CompletionStatus = "canceled"
	CompletionFailed    CompletionStatus = "failed"
)

type Completion struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Status       CompletionStatus
	Error        *CanonicalError
}

// StreamChunk is the canonical streaming boundary. Buffered marks the current
// provider behavior: CWapi emits a completed response as a legal stream, but
// does not claim that it received token-level deltas from Web GPT.
type StreamChunk struct {
	Role           Role
	ContentDelta   string
	ToolCallDeltas []ToolCallDelta
	FinishReason   string
	Buffered       bool
	Done           bool
	Error          *CanonicalError
}

type ToolCallDelta struct {
	Index          int
	ID             string
	Name           string
	ArgumentsDelta string
}

type CompletionMetadata struct {
	ID      string
	Model   string
	Created time.Time
}

type ErrorKind string

const (
	ErrorExternalRequest ErrorKind = "external_request"
	ErrorCapability      ErrorKind = "capability"
	ErrorCanonical       ErrorKind = "canonical_conversion"
	ErrorBridge          ErrorKind = "mcp_conversion"
	ErrorWebGPT          ErrorKind = "web_gpt_response"
	ErrorToolMapping     ErrorKind = "tool_mapping"
	ErrorStream          ErrorKind = "stream"
)

type CanonicalError struct {
	Code      string
	Kind      ErrorKind
	Detail    string
	Retryable bool
}

func (e *CanonicalError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

type Adapter interface {
	Name() string
	Capabilities() Capabilities
	DecodeRequest([]byte) (Conversation, error)
	EncodeCompletion(Completion, CompletionMetadata) (map[string]any, error)
	DecodeStreamChunk([]byte) (StreamChunk, error)
	EncodeStreamChunk(StreamChunk, CompletionMetadata) (map[string]any, error)
}

func BufferedCompletionChunks(completion Completion) []StreamChunk {
	first := StreamChunk{Role: RoleAssistant, ContentDelta: completion.Content, Buffered: true}
	for index, call := range completion.ToolCalls {
		first.ToolCallDeltas = append(first.ToolCallDeltas, ToolCallDelta{
			Index: index, ID: call.ID, Name: call.Name, ArgumentsDelta: canonicalJSONText(call.Arguments),
		})
	}
	return []StreamChunk{
		first,
		{FinishReason: completion.FinishReason, Buffered: true},
		{Done: true, Buffered: true},
	}
}
