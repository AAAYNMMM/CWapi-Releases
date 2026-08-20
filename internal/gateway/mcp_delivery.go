package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

const (
	MaxSlackArtifactBytes       = 8 * 1024 * 1024
	maxMCPDeliveryArtifacts     = 16
	inlineMCPTextBytes          = 12 * 1024
	inlineMCPResultBytes        = 24 * 1024
	maxEncodedArtifactInputSize = ((MaxSlackArtifactBytes + 2) / 3 * 4) + 16
)

var (
	errMCPArtifactTooLarge = errors.New("MCP_DELIVERY_ARTIFACT_TOO_LARGE")
	errMCPArtifactLimit    = errors.New("MCP_DELIVERY_ARTIFACT_LIMIT")
)

type mcpDeliveryArtifact struct {
	Name      string
	MediaType string
	Data      []byte
}

// externalizeMCPResult enforces CWapi's outbound policy after Codex/MCP has
// already produced content. It never opens a local path or follows a resource
// URI itself. Local read authority therefore stays with Codex/MCP, while CWapi
// independently decides whether returned bytes may leave through Slack.
func (g *Gateway) externalizeMCPResult(ctx context.Context, message slackcore.Message, response protocol.MCPResponse) protocol.MCPResponse {
	if response.Status != protocol.MCPStatusCompleted || len(response.Result) == 0 {
		return response
	}

	var value any
	decoder := json.NewDecoder(strings.NewReader(string(response.Result)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return mcpErrorResponse(response.RequestID, protocol.MCPStatusFailed,
			"MCP_DELIVERY_RESULT_INVALID", "delivery", "MCP result is not valid JSON")
	}

	artifacts := make([]mcpDeliveryArtifact, 0, 4)
	normalized, err := normalizeMCPDeliveryValue(value, response.RequestID, &artifacts)
	if err != nil {
		return mcpDeliveryErrorResponse(response.RequestID, err)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return mcpErrorResponse(response.RequestID, protocol.MCPStatusFailed,
			"MCP_DELIVERY_RESULT_ENCODE_FAILED", "delivery", "MCP result could not be prepared for Slack delivery")
	}

	if len(encoded) > inlineMCPResultBytes {
		if err := appendMCPArtifact(&artifacts, mcpDeliveryArtifact{
			Name:      response.RequestID + "-result.json",
			MediaType: "application/json",
			Data:      encoded,
		}); err != nil {
			return mcpDeliveryErrorResponse(response.RequestID, err)
		}
		encoded, _ = json.Marshal(map[string]any{
			"cwapi_delivery": "slack_files",
			"artifact_count": len(artifacts),
			"note":           "MCP result exceeded the inline response budget and was delivered as Slack file content.",
		})
	}

	if len(artifacts) == 0 {
		response.Result = encoded
		return response
	}
	filePoster, ok := g.poster.(SlackFilePoster)
	if !ok {
		return mcpErrorResponse(response.RequestID, protocol.MCPStatusUnavailable,
			"MCP_SLACK_FILE_DELIVERY_UNAVAILABLE", "delivery", "Slack file delivery is not available")
	}

	resources := append([]protocol.MCPResourceRef(nil), response.Resources...)
	threadTS := rootThread(message)
	for _, artifact := range artifacts {
		uploaded, err := filePoster.UploadFile(ctx, artifact.Name, artifact.MediaType, artifact.Data, threadTS)
		if err != nil {
			if strings.Contains(err.Error(), "TOO_LARGE") {
				return mcpDeliveryErrorResponse(response.RequestID, errMCPArtifactTooLarge)
			}
			return mcpErrorResponse(response.RequestID, protocol.MCPStatusFailed,
				"MCP_SLACK_FILE_DELIVERY_FAILED", "delivery", err.Error())
		}
		uri := strings.TrimSpace(uploaded.Permalink)
		if uri == "" {
			uri = "slack-file://" + strings.TrimSpace(uploaded.FileID)
		}
		digest := sha256.Sum256(artifact.Data)
		resources = append(resources, protocol.MCPResourceRef{
			URI:       uri,
			MediaType: artifact.MediaType,
			SHA256:    hex.EncodeToString(digest[:]),
			SizeBytes: int64(len(artifact.Data)),
		})
	}
	response.Result = encoded
	response.Resources = resources
	return response
}

func normalizeMCPDeliveryValue(value any, requestID string, artifacts *[]mcpDeliveryArtifact) (any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}
	if raw, ok := object["content"].([]any); ok {
		normalized, err := normalizeMCPToolContent(raw, requestID, artifacts)
		if err != nil {
			return nil, err
		}
		object["content"] = normalized
	}
	if raw, ok := object["contents"].([]any); ok {
		normalized, err := normalizeMCPResourceContents(raw, requestID, artifacts)
		if err != nil {
			return nil, err
		}
		object["contents"] = normalized
	}
	return object, nil
}

func normalizeMCPToolContent(items []any, requestID string, artifacts *[]mcpDeliveryArtifact) ([]any, error) {
	result := make([]any, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			result = append(result, raw)
			continue
		}
		typeName, _ := item["type"].(string)
		switch typeName {
		case "text":
			text, _ := item["text"].(string)
			if len([]byte(text)) <= inlineMCPTextBytes {
				result = append(result, item)
				continue
			}
			name := nextArtifactName(requestID, "text", "text/plain", len(*artifacts)+1)
			if err := appendMCPArtifact(artifacts, mcpDeliveryArtifact{Name: name, MediaType: "text/plain", Data: []byte(text)}); err != nil {
				return nil, err
			}
			result = append(result, map[string]any{"type": "text", "text": "[CWapi delivered long text as a Slack file.]"})
		case "image", "audio":
			encoded, _ := item["data"].(string)
			mediaType := mcpMediaType(item)
			data, err := decodeMCPBase64(encoded)
			if err != nil {
				return nil, err
			}
			name := nextArtifactName(requestID, typeName, mediaType, len(*artifacts)+1)
			if err := appendMCPArtifact(artifacts, mcpDeliveryArtifact{Name: name, MediaType: mediaType, Data: data}); err != nil {
				return nil, err
			}
			result = append(result, map[string]any{"type": "text", "text": "[CWapi delivered " + typeName + " content as a Slack file.]"})
		case "resource":
			resource, ok := item["resource"].(map[string]any)
			if !ok {
				result = append(result, item)
				continue
			}
			normalized, changed, err := normalizeMCPResource(resource, requestID, artifacts)
			if err != nil {
				return nil, err
			}
			if changed {
				item["resource"] = normalized
			}
			result = append(result, item)
		default:
			result = append(result, item)
		}
	}
	return result, nil
}

func normalizeMCPResourceContents(items []any, requestID string, artifacts *[]mcpDeliveryArtifact) ([]any, error) {
	result := make([]any, 0, len(items))
	for _, raw := range items {
		resource, ok := raw.(map[string]any)
		if !ok {
			result = append(result, raw)
			continue
		}
		normalized, _, err := normalizeMCPResource(resource, requestID, artifacts)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeMCPResource(resource map[string]any, requestID string, artifacts *[]mcpDeliveryArtifact) (map[string]any, bool, error) {
	uri, _ := resource["uri"].(string)
	mediaType := mcpMediaType(resource)
	if blob, ok := resource["blob"].(string); ok && blob != "" {
		data, err := decodeMCPBase64(blob)
		if err != nil {
			return nil, false, err
		}
		name := resourceArtifactName(requestID, uri, mediaType, len(*artifacts)+1)
		if err := appendMCPArtifact(artifacts, mcpDeliveryArtifact{Name: name, MediaType: mediaType, Data: data}); err != nil {
			return nil, false, err
		}
		copy := cloneJSONMap(resource)
		delete(copy, "blob")
		copy["cwapi_delivery"] = "slack_file"
		return copy, true, nil
	}
	if text, ok := resource["text"].(string); ok {
		if mediaType == "application/octet-stream" {
			mediaType = "text/plain"
		}
		name := resourceArtifactName(requestID, uri, mediaType, len(*artifacts)+1)
		if err := appendMCPArtifact(artifacts, mcpDeliveryArtifact{Name: name, MediaType: mediaType, Data: []byte(text)}); err != nil {
			return nil, false, err
		}
		copy := cloneJSONMap(resource)
		delete(copy, "text")
		copy["cwapi_delivery"] = "slack_file"
		return copy, true, nil
	}
	return resource, false, nil
}

func appendMCPArtifact(target *[]mcpDeliveryArtifact, artifact mcpDeliveryArtifact) error {
	if len(artifact.Data) == 0 {
		return errors.New("MCP_DELIVERY_ARTIFACT_EMPTY")
	}
	if len(artifact.Data) > MaxSlackArtifactBytes {
		return errMCPArtifactTooLarge
	}
	if len(*target) >= maxMCPDeliveryArtifacts {
		return errMCPArtifactLimit
	}
	artifact.MediaType = strings.TrimSpace(artifact.MediaType)
	if artifact.MediaType == "" {
		artifact.MediaType = "application/octet-stream"
	}
	*target = append(*target, artifact)
	return nil
}

func decodeMCPBase64(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("MCP_DELIVERY_BASE64_EMPTY")
	}
	if len(encoded) > maxEncodedArtifactInputSize {
		return nil, errMCPArtifactTooLarge
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("MCP_DELIVERY_BASE64_INVALID")
	}
	if len(data) > MaxSlackArtifactBytes {
		return nil, errMCPArtifactTooLarge
	}
	return data, nil
}

func mcpDeliveryErrorResponse(requestID string, err error) protocol.MCPResponse {
	switch {
	case errors.Is(err, errMCPArtifactTooLarge):
		return mcpErrorResponse(requestID, protocol.MCPStatusFailed,
			"MCP_DELIVERY_FILE_TOO_LARGE", "delivery", "CWapi refuses Slack files larger than 8 MiB")
	case errors.Is(err, errMCPArtifactLimit):
		return mcpErrorResponse(requestID, protocol.MCPStatusFailed,
			"MCP_DELIVERY_FILE_LIMIT", "delivery", "CWapi refuses more than 16 Slack files from one MCP response")
	default:
		return mcpErrorResponse(requestID, protocol.MCPStatusFailed,
			"MCP_DELIVERY_CONTENT_INVALID", "delivery", err.Error())
	}
}

func mcpMediaType(value map[string]any) string {
	for _, key := range []string{"mimeType", "mime_type", "mediaType", "media_type"} {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return "application/octet-stream"
}

func resourceArtifactName(requestID, rawURI, mediaType string, index int) string {
	preferred := "resource"
	if parsed, err := url.Parse(strings.TrimSpace(rawURI)); err == nil {
		if base := path.Base(parsed.Path); base != "" && base != "." && base != "/" {
			preferred = base
		}
	}
	return nextArtifactName(requestID, preferred, mediaType, index)
}

func nextArtifactName(requestID, preferred, mediaType string, index int) string {
	preferred = sanitizeArtifactName(preferred)
	if preferred == "" {
		preferred = "artifact"
	}
	ext := artifactExtension(mediaType)
	if dot := strings.LastIndex(preferred, "."); dot > 0 {
		ext = ""
	}
	return requestID + "-" + strconv.Itoa(index) + "-" + preferred + ext
}

func sanitizeArtifactName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\r", "_")
	value = strings.ReplaceAll(value, "\n", "_")
	if len(value) > 96 {
		value = value[:96]
	}
	return strings.Trim(value, ". ")
}

func artifactExtension(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0])) {
	case "text/plain":
		return ".txt"
	case "application/json":
		return ".json"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	default:
		return ".bin"
	}
}

func cloneJSONMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source)+1)
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func rootThread(message slackcore.Message) string {
	if value := strings.TrimSpace(message.ThreadTS); value != "" {
		return value
	}
	return strings.TrimSpace(message.MessageTS)
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func artifactDebugName(artifact mcpDeliveryArtifact) string {
	return fmt.Sprintf("%s (%s, %d bytes)", artifact.Name, artifact.MediaType, len(artifact.Data))
}
