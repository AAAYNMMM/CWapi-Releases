package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

const mcpRelayTimeout = 120 * time.Second

var allowedCodexMCPMethods = map[string]struct{}{
	"projects/list":           {},
	"mcpServerStatus/list":    {},
	"mcpServer/resource/read": {},
	"mcpServer/tool/call":     {},
}

func (g *Gateway) handleMCPMessage(ctx context.Context, message slackcore.Message, subject protocol.MCPSubject) error {
	if subject.Family != protocol.MCPFamilyRequest {
		return g.postTransientMCPError(ctx, message, subject.RequestID, protocol.MCPStatusFailed,
			"MCP_REQUEST_FAMILY_REQUIRED", "protocol", "CWapi accepts MCP_REQUEST messages from callers. Send method=projects/list with params={} to discover project_id and usage.")
	}
	return g.handleMCPRequest(ctx, message, subject)
}

func (g *Gateway) handleMCPRequest(ctx context.Context, message slackcore.Message, subject protocol.MCPSubject) error {
	startedAt := nowMillis()
	request, err := protocol.DecodeMCPRequest([]byte(message.Body))
	if err != nil {
		return g.postTransientMCPError(ctx, message, subject.RequestID, protocol.MCPStatusFailed,
			"MCP_REQUEST_INVALID", "protocol", "MCP request validation failed. Send method=projects/list with params={} in a complete CWapi MCP v1 frame to discover project_id and usage.")
	}
	if request.RequestID != subject.RequestID {
		return g.postTransientMCPError(ctx, message, subject.RequestID, protocol.MCPStatusFailed,
			"MCP_REQUEST_ID_MISMATCH", "protocol", "MCP subject and body request IDs do not match")
	}
	fingerprint, err := request.Fingerprint()
	if err != nil {
		return g.postTransientMCPError(ctx, message, subject.RequestID, protocol.MCPStatusFailed,
			"MCP_REQUEST_FINGERPRINT_FAILED", "protocol", "MCP request fingerprint could not be created")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("MCP_REQUEST_ENCODE_FAILED: %w", err)
	}
	now := nowMillis()
	source := mcpSourceIdentity(message)
	stored, insertedRequest, err := g.store.InsertMCPRequestIfAbsent(ctx, state.MCPRequestRecord{
		RequestID:       request.RequestID,
		SourceIdentity:  source,
		SourceMessageID: source,
		Method:          request.Method,
		ArgumentsHash:   fingerprint,
		RequestJSON:     string(requestJSON),
		ExecutionState:  state.MCPExecutionReceived,
		DeliveryState:   state.MCPDeliveryPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return err
	}
	if !insertedRequest {
		if stored.Method != request.Method || stored.ArgumentsHash != fingerprint {
			return g.postTransientMCPError(ctx, message, subject.RequestID, protocol.MCPStatusFailed,
				"MCP_REQUEST_ID_CONFLICT", "idempotency", "request_id was already used for a different MCP request")
		}
		if stored.ResponseJSON != "" {
			if stored.SourceMessageID == source && stored.DeliveryState == state.MCPDeliveryDelivered {
				return nil
			}
			return g.deliverStoredMCPResponse(ctx, message, stored)
		}
		if stored.SourceMessageID == source {
			return nil
		}
		return nil
	}
	g.emitMCPExecution(request, "request", state.MCPExecutionReceived, "MCP request accepted", startedAt, nil)

	if request.ProjectID != "" {
		if err := g.updateMCPExecution(ctx, request, state.MCPExecutionPreparing, "Preparing exact-commit workspace", startedAt); err != nil {
			return err
		}
	}
	execution, release, response := g.prepareMCPExecutionContext(ctx, request)
	if release != nil {
		defer release()
	}
	if response != nil {
		return g.completeAndDeliverMCPResponse(ctx, message, request, *response, startedAt)
	}
	if err := g.updateMCPExecution(ctx, request, state.MCPExecutionRunning, "Calling MCP runtime", startedAt); err != nil {
		return err
	}

	result := g.relayCodexMCP(ctx, request, execution)
	g.emitMCPProcessState(request, result, startedAt)
	result = g.externalizeMCPResult(ctx, message, result)
	return g.completeAndDeliverMCPResponse(ctx, message, request, result, startedAt)
}

func (g *Gateway) updateMCPExecution(ctx context.Context, request protocol.MCPRequest, status, message string, startedAt int64) error {
	if err := g.store.UpdateMCPExecution(ctx, request.RequestID, status, nowMillis()); err != nil {
		return err
	}
	g.emitMCPExecution(request, status, status, message, startedAt, map[string]any{"execution_state": status})
	return nil
}

func (g *Gateway) prepareMCPExecutionContext(ctx context.Context, request protocol.MCPRequest) (MCPExecutionContext, func(), *protocol.MCPResponse) {
	if request.ProjectID == "" {
		return MCPExecutionContext{}, nil, nil
	}
	runtime := g.mcpRuntimeSnapshot()
	if runtime.ContextResolver == nil {
		response := mcpErrorResponse(request.RequestID, protocol.MCPStatusUnavailable,
			"MCP_PROJECT_CONTEXT_UNAVAILABLE", "workspace", "CWapi exact-commit project context is unavailable")
		return MCPExecutionContext{}, nil, &response
	}
	execution, release, err := runtime.ContextResolver.PrepareMCPContext(
		ctx,
		request.RequestID,
		request.ProjectID,
		request.ExpectedCommit,
	)
	if err != nil {
		detail := err.Error()
		if strings.Contains(detail, "MCP_PROJECT_NOT_FOUND") {
			detail += ". " + g.projectDiscoverySummary()
		}
		response := mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_PROJECT_PREPARE_FAILED", "workspace", detail)
		return MCPExecutionContext{}, release, &response
	}
	if execution.ProjectID != request.ProjectID || !strings.EqualFold(execution.ExpectedCommit, request.ExpectedCommit) || !filepath.IsAbs(execution.CWD) {
		response := mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_PROJECT_CONTEXT_MISMATCH", "workspace", "prepared workspace did not match the requested project_id and expected_commit")
		return MCPExecutionContext{}, release, &response
	}
	return execution, release, nil
}

func (g *Gateway) relayCodexMCP(ctx context.Context, request protocol.MCPRequest, execution MCPExecutionContext) protocol.MCPResponse {
	method := strings.TrimSpace(request.Method)
	if _, ok := allowedCodexMCPMethods[method]; !ok {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusUnavailable,
			"MCP_METHOD_UNAVAILABLE", "relay", "only stock Codex app-server MCP methods are exposed")
	}
	if method == "projects/list" {
		return g.projectsListResponse(request)
	}
	runtime := g.mcpRuntimeSnapshot()
	if runtime.Toolhost == nil {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusUnavailable,
			"MCP_TOOLHOST_UNAVAILABLE", "toolhost", "Codex MCP relay is not attached")
	}

	params := map[string]any{}
	if len(request.Params) > 0 && string(request.Params) != "null" {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
				"MCP_PARAMS_INVALID", "protocol", "MCP params must be a JSON object")
		}
	}
	if _, supplied := params["threadId"]; supplied {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_THREAD_ID_MANAGED", "relay", "threadId is owned by CWapi")
	}
	if response := g.prepareCWapiProcessCall(request, execution, params); response != nil {
		return *response
	}

	value, err := runtime.Toolhost.CallMCP(ctx, method, params, mcpRelayTimeout, execution)
	if err != nil {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusUnavailable,
			"MCP_TOOLHOST_CALL_FAILED", "toolhost", err.Error())
	}
	if method == "mcpServerStatus/list" {
		value = g.withCWapiDiscovery(value)
	}
	if method == "mcpServer/tool/call" {
		if message, failed := mcpToolFailure(value); failed {
			return mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
				"MCP_TOOL_REPORTED_ERROR", "tool", message)
		}
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_RESULT_ENCODE_FAILED", "relay", "Codex MCP result could not be encoded")
	}
	return protocol.MCPResponse{
		Schema:          protocol.MCPResponseSchema,
		ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID:       request.RequestID,
		Status:          protocol.MCPStatusCompleted,
		Result:          payload,
	}
}

func mcpErrorResponse(requestID string, status protocol.MCPStatus, code, category, text string) protocol.MCPResponse {
	text = boundedMCPText(strings.TrimSpace(text), protocol.MaxMCPErrorMessageBytes)
	if text == "" {
		text = code
	}
	return protocol.MCPResponse{
		Schema:          protocol.MCPResponseSchema,
		ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID:       requestID,
		Status:          status,
		Error: &protocol.MCPError{
			Code:      code,
			Category:  category,
			Message:   text,
			Retryable: false,
		},
	}
}

func (g *Gateway) completeAndDeliverMCPResponse(ctx context.Context, message slackcore.Message, request protocol.MCPRequest, response protocol.MCPResponse, startedAt int64) error {
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("MCP_RESPONSE_ENCODE_FAILED: %w", err)
	}
	if _, err := protocol.DecodeMCPResponse(responseJSON); err != nil {
		return fmt.Errorf("MCP_RESPONSE_INVALID: %w", err)
	}
	if err := g.store.CompleteMCPRequest(ctx, response.RequestID, string(response.Status), string(responseJSON), nowMillis()); err != nil {
		return err
	}
	terminalMessage := "MCP execution completed"
	fields := map[string]any{"execution_state": response.Status}
	if response.Error != nil {
		terminalMessage = response.Error.Message
		fields["error_code"] = response.Error.Code
		fields["error_category"] = response.Error.Category
	}
	g.emitMCPExecution(request, "terminal", string(response.Status), terminalMessage, startedAt, fields)
	return g.deliverMCPResponse(ctx, message, response, true)
}

func (g *Gateway) deliverStoredMCPResponse(ctx context.Context, message slackcore.Message, record state.MCPRequestRecord) error {
	response, err := protocol.DecodeMCPResponse([]byte(record.ResponseJSON))
	if err != nil {
		return fmt.Errorf("MCP_STORED_RESPONSE_INVALID: %w", err)
	}
	return g.deliverMCPResponse(ctx, message, response, true)
}

func (g *Gateway) deliverMCPResponse(ctx context.Context, message slackcore.Message, response protocol.MCPResponse, persistDelivery bool) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("MCP_RESPONSE_ENCODE_FAILED: %w", err)
	}
	subject, err := protocol.BuildMCPSubject(protocol.MCPFamilyResponse, response.RequestID)
	if err != nil {
		return err
	}
	_, postErr := g.poster.Post(ctx, subject, string(payload), rootThread(message))
	if !persistDelivery {
		if postErr != nil {
			return fmt.Errorf("MCP_RESPONSE_DELIVERY_FAILED: %w", postErr)
		}
		return nil
	}
	if postErr != nil {
		_ = g.store.UpdateMCPDelivery(ctx, response.RequestID, state.MCPDeliveryAttention, nowMillis())
		g.emitMCPDelivery(response.RequestID, state.MCPDeliveryAttention, "MCP result delivery needs attention")
		return fmt.Errorf("MCP_RESPONSE_DELIVERY_FAILED: %w", postErr)
	}
	if err := g.store.UpdateMCPDelivery(ctx, response.RequestID, state.MCPDeliveryDelivered, nowMillis()); err != nil {
		return err
	}
	g.emitMCPDelivery(response.RequestID, state.MCPDeliveryDelivered, "MCP result delivered to Slack")
	return nil
}

func (g *Gateway) postTransientMCPError(ctx context.Context, message slackcore.Message, requestID string, status protocol.MCPStatus, code, category, text string) error {
	return g.deliverMCPResponse(ctx, message, mcpErrorResponse(requestID, status, code, category, text), false)
}

func mcpSourceIdentity(message slackcore.Message) string {
	if message.MessageID != "" {
		return message.MessageID
	}
	return slackcore.MessageID(message.ChannelID, message.MessageTS)
}
