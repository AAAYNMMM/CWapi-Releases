package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	MCPProtocolVersion = "cwapi-mcp/1"
	MCPRequestSchema   = "cwapi.mcp.request.v1"
	MCPResponseSchema  = "cwapi.mcp.response.v1"
	MCPEventSchema     = "cwapi.mcp.event.v1"

	MaxMCPMessageBytes      = 64 * 1024
	MaxMCPBodyBytes         = 32 * 1024
	MaxMCPErrorMessageBytes = 4 * 1024
)

var (
	// Codex app-server uses camelCase method segments such as mcpServer/tool/call.
	mcpMethodPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_.-]*(?:/[a-z][A-Za-z0-9_.-]*)*$`)
	mcpEventPattern  = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
	mcpCodePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,127}$`)
	mcpProjectID     = regexp.MustCompile(`^prj-[a-f0-9]{24}$`)
	mcpCommitSHA     = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

type MCPFamily string

const (
	MCPFamilyRequest  MCPFamily = "MCP_REQUEST"
	MCPFamilyResponse MCPFamily = "MCP_RESPONSE"
	MCPFamilyEvent    MCPFamily = "MCP_EVENT"
)

type MCPSubject struct {
	Family    MCPFamily
	RequestID string
}

func BuildMCPSubject(family MCPFamily, requestID string) (string, error) {
	if !validMCPFamily(family) {
		return "", fmt.Errorf("MCP_FAMILY_UNSUPPORTED: %s", family)
	}
	if !identityPattern.MatchString(requestID) {
		return "", fmt.Errorf("MCP_REQUEST_ID_INVALID: %q", requestID)
	}
	return fmt.Sprintf("[CWapi/MCP/1][%s][%s]", family, requestID), nil
}

func ParseMCPSubject(value string) (MCPSubject, error) {
	if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n") {
		return MCPSubject{}, errors.New("MCP_SUBJECT_INVALID")
	}
	if !strings.HasPrefix(value, "[CWapi/MCP/1][") || !strings.HasSuffix(value, "]") {
		return MCPSubject{}, errors.New("MCP_SUBJECT_INVALID")
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(value, "[CWapi/MCP/1]["), "]")
	parts := strings.Split(inner, "][")
	if len(parts) != 2 {
		return MCPSubject{}, errors.New("MCP_SUBJECT_INVALID")
	}
	subject := MCPSubject{Family: MCPFamily(parts[0]), RequestID: parts[1]}
	if !validMCPFamily(subject.Family) {
		return MCPSubject{}, fmt.Errorf("MCP_FAMILY_UNSUPPORTED: %s", subject.Family)
	}
	if !identityPattern.MatchString(subject.RequestID) {
		return MCPSubject{}, errors.New("MCP_REQUEST_ID_INVALID")
	}
	return subject, nil
}

func validMCPFamily(family MCPFamily) bool {
	switch family {
	case MCPFamilyRequest, MCPFamilyResponse, MCPFamilyEvent:
		return true
	default:
		return false
	}
}

type MCPRequest struct {
	Schema          string          `json:"schema"`
	ProtocolVersion string          `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	ProjectID       string          `json:"project_id,omitempty"`
	ExpectedCommit  string          `json:"expected_commit,omitempty"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params"`
}

func DecodeMCPRequest(data []byte) (MCPRequest, error) {
	if len(data) == 0 || len(data) > MaxMCPMessageBytes {
		return MCPRequest{}, fmt.Errorf("MCP_REQUEST_SIZE_INVALID: %d", len(data))
	}
	var request MCPRequest
	if err := decodeStrict(data, &request); err != nil {
		return MCPRequest{}, fmt.Errorf("MCP_REQUEST_JSON_INVALID: %w", err)
	}
	if request.Schema != MCPRequestSchema {
		return MCPRequest{}, fmt.Errorf("MCP_REQUEST_SCHEMA_UNSUPPORTED: %q", request.Schema)
	}
	if request.ProtocolVersion != MCPProtocolVersion {
		return MCPRequest{}, fmt.Errorf("MCP_PROTOCOL_VERSION_UNSUPPORTED: %q", request.ProtocolVersion)
	}
	if !identityPattern.MatchString(request.RequestID) {
		return MCPRequest{}, errors.New("MCP_REQUEST_ID_INVALID")
	}
	if len(request.Method) > 128 || !mcpMethodPattern.MatchString(request.Method) {
		return MCPRequest{}, fmt.Errorf("MCP_METHOD_INVALID: %q", request.Method)
	}
	if (request.ProjectID == "") != (request.ExpectedCommit == "") {
		return MCPRequest{}, errors.New("MCP_PROJECT_CONTEXT_INCOMPLETE")
	}
	if request.ProjectID != "" {
		if request.ProjectID != strings.TrimSpace(request.ProjectID) || !mcpProjectID.MatchString(request.ProjectID) {
			return MCPRequest{}, errors.New("MCP_PROJECT_ID_INVALID")
		}
		if request.ExpectedCommit != strings.TrimSpace(request.ExpectedCommit) || !mcpCommitSHA.MatchString(request.ExpectedCommit) {
			return MCPRequest{}, errors.New("MCP_EXPECTED_COMMIT_INVALID")
		}
		request.ExpectedCommit = strings.ToLower(request.ExpectedCommit)
	}
	params, err := canonicalJSONObject(request.Params, MaxMCPBodyBytes, "MCP_PARAMS")
	if err != nil {
		return MCPRequest{}, err
	}
	request.Params = params
	return request, nil
}

func (r MCPRequest) Fingerprint() (string, error) {
	if !identityPattern.MatchString(r.RequestID) || !mcpMethodPattern.MatchString(r.Method) {
		return "", errors.New("MCP_REQUEST_INVALID")
	}
	if (r.ProjectID == "") != (r.ExpectedCommit == "") {
		return "", errors.New("MCP_PROJECT_CONTEXT_INCOMPLETE")
	}
	if r.ProjectID != "" && (!mcpProjectID.MatchString(r.ProjectID) || !mcpCommitSHA.MatchString(r.ExpectedCommit)) {
		return "", errors.New("MCP_PROJECT_CONTEXT_INVALID")
	}
	params, err := canonicalJSONObject(r.Params, MaxMCPBodyBytes, "MCP_PARAMS")
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		ProjectID      string          `json:"project_id,omitempty"`
		ExpectedCommit string          `json:"expected_commit,omitempty"`
		Method         string          `json:"method"`
		Params         json.RawMessage `json:"params"`
	}{ProjectID: r.ProjectID, ExpectedCommit: strings.ToLower(r.ExpectedCommit), Method: r.Method, Params: params})
	if err != nil {
		return "", fmt.Errorf("MCP_FINGERPRINT_ENCODE_FAILED: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

type MCPStatus string

const (
	MCPStatusCompleted   MCPStatus = "completed"
	MCPStatusBlocked     MCPStatus = "blocked"
	MCPStatusFailed      MCPStatus = "failed"
	MCPStatusTimedOut    MCPStatus = "timed_out"
	MCPStatusUnavailable MCPStatus = "unavailable"
)

func (s MCPStatus) Terminal() bool {
	switch s {
	case MCPStatusCompleted, MCPStatusBlocked, MCPStatusFailed, MCPStatusTimedOut, MCPStatusUnavailable:
		return true
	default:
		return false
	}
}

type MCPError struct {
	Code               string `json:"code"`
	Category           string `json:"category"`
	Message            string `json:"message"`
	Retryable          bool   `json:"retryable"`
	RequiredCapability string `json:"required_capability,omitempty"`
	MissingRuntime     string `json:"missing_runtime,omitempty"`
	MissingTool        string `json:"missing_tool,omitempty"`
}

type MCPResourceRef struct {
	URI       string `json:"uri"`
	MediaType string `json:"media_type,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type MCPResponse struct {
	Schema          string           `json:"schema"`
	ProtocolVersion string           `json:"protocol_version"`
	RequestID       string           `json:"request_id"`
	Status          MCPStatus        `json:"status"`
	Result          json.RawMessage  `json:"result,omitempty"`
	Error           *MCPError        `json:"error,omitempty"`
	Resources       []MCPResourceRef `json:"resources,omitempty"`
}

func DecodeMCPResponse(data []byte) (MCPResponse, error) {
	if len(data) == 0 || len(data) > MaxMCPMessageBytes {
		return MCPResponse{}, fmt.Errorf("MCP_RESPONSE_SIZE_INVALID: %d", len(data))
	}
	var response MCPResponse
	if err := decodeStrict(data, &response); err != nil {
		return MCPResponse{}, fmt.Errorf("MCP_RESPONSE_JSON_INVALID: %w", err)
	}
	if response.Schema != MCPResponseSchema || response.ProtocolVersion != MCPProtocolVersion {
		return MCPResponse{}, errors.New("MCP_RESPONSE_VERSION_UNSUPPORTED")
	}
	if !identityPattern.MatchString(response.RequestID) || !response.Status.Terminal() {
		return MCPResponse{}, errors.New("MCP_RESPONSE_ID_OR_STATUS_INVALID")
	}
	if len(response.Result) > 0 {
		result, err := canonicalJSONObject(response.Result, MaxMCPBodyBytes, "MCP_RESULT")
		if err != nil {
			return MCPResponse{}, err
		}
		response.Result = result
	}
	if err := validateMCPError(response.Status, response.Error); err != nil {
		return MCPResponse{}, err
	}
	for _, resource := range response.Resources {
		if err := validateMCPResource(resource); err != nil {
			return MCPResponse{}, err
		}
	}
	return response, nil
}

func validateMCPError(status MCPStatus, value *MCPError) error {
	if status == MCPStatusCompleted {
		if value != nil {
			return errors.New("MCP_COMPLETED_RESPONSE_HAS_ERROR")
		}
		return nil
	}
	if value == nil {
		return errors.New("MCP_NON_COMPLETED_RESPONSE_REQUIRES_ERROR")
	}
	if !mcpCodePattern.MatchString(value.Code) || value.Category == "" || len(value.Category) > 64 {
		return errors.New("MCP_ERROR_METADATA_INVALID")
	}
	if len([]byte(value.Message)) > MaxMCPErrorMessageBytes {
		return errors.New("MCP_ERROR_MESSAGE_TOO_LARGE")
	}
	return nil
}

func validateMCPResource(value MCPResourceRef) error {
	if value.URI == "" || len(value.URI) > 2048 || strings.ContainsAny(value.URI, "\r\n") {
		return errors.New("MCP_RESOURCE_URI_INVALID")
	}
	if value.SHA256 != "" {
		if len(value.SHA256) != 64 {
			return errors.New("MCP_RESOURCE_HASH_INVALID")
		}
		if _, err := hex.DecodeString(value.SHA256); err != nil {
			return errors.New("MCP_RESOURCE_HASH_INVALID")
		}
	}
	if value.SizeBytes < 0 {
		return errors.New("MCP_RESOURCE_SIZE_INVALID")
	}
	return nil
}

type MCPEvent struct {
	Schema          string          `json:"schema"`
	ProtocolVersion string          `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	Sequence        uint64          `json:"sequence"`
	Event           string          `json:"event"`
	Data            json.RawMessage `json:"data"`
}

func DecodeMCPEvent(data []byte) (MCPEvent, error) {
	if len(data) == 0 || len(data) > MaxMCPMessageBytes {
		return MCPEvent{}, fmt.Errorf("MCP_EVENT_SIZE_INVALID: %d", len(data))
	}
	var event MCPEvent
	if err := decodeStrict(data, &event); err != nil {
		return MCPEvent{}, fmt.Errorf("MCP_EVENT_JSON_INVALID: %w", err)
	}
	if event.Schema != MCPEventSchema || event.ProtocolVersion != MCPProtocolVersion {
		return MCPEvent{}, errors.New("MCP_EVENT_VERSION_UNSUPPORTED")
	}
	if !identityPattern.MatchString(event.RequestID) || !mcpEventPattern.MatchString(event.Event) {
		return MCPEvent{}, errors.New("MCP_EVENT_METADATA_INVALID")
	}
	payload, err := canonicalJSONObject(event.Data, MaxMCPBodyBytes, "MCP_EVENT_DATA")
	if err != nil {
		return MCPEvent{}, err
	}
	event.Data = payload
	return event, nil
}

func canonicalJSONObject(raw json.RawMessage, maxBytes int, label string) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("%s_TOO_LARGE: %d", label, len(raw))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s_INVALID: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%s_INVALID: trailing JSON value", label)
		}
		return nil, fmt.Errorf("%s_INVALID: %w", label, err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("%s_MUST_BE_OBJECT", label)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%s_ENCODE_FAILED: %w", label, err)
	}
	return encoded, nil
}
