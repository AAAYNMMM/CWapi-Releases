package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

type deliveryTestPoster struct {
	uploads []mcpDeliveryArtifact
	threads []string
}

func (p *deliveryTestPoster) Post(context.Context, string, string, string) (PostedMessage, error) {
	return PostedMessage{MessageID: "posted", MessageTS: "2.000"}, nil
}

func (p *deliveryTestPoster) UploadFile(_ context.Context, filename, mediaType string, data []byte, threadTS string) (UploadedFile, error) {
	p.uploads = append(p.uploads, mcpDeliveryArtifact{Name: filename, MediaType: mediaType, Data: append([]byte(nil), data...)})
	p.threads = append(p.threads, threadTS)
	index := len(p.uploads)
	return UploadedFile{
		FileID:    "F" + string(rune('0'+index)),
		Name:      filename,
		Size:      int64(len(data)),
		Permalink: "https://example.slack.com/files/F" + string(rune('0'+index)),
	}, nil
}

func completedDeliveryResponse(requestID string, result any) protocol.MCPResponse {
	payload, _ := json.Marshal(result)
	return protocol.MCPResponse{
		Schema:          protocol.MCPResponseSchema,
		ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID:       requestID,
		Status:          protocol.MCPStatusCompleted,
		Result:          payload,
	}
}

func TestExternalizeMCPResultKeepsShortTextInline(t *testing.T) {
	poster := &deliveryTestPoster{}
	gateway := &Gateway{poster: poster}
	response := gateway.externalizeMCPResult(context.Background(), slackcore.Message{MessageTS: "1.000"}, completedDeliveryResponse("REQ-SHORT", map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "ok"}},
	}))
	if response.Status != protocol.MCPStatusCompleted || len(response.Resources) != 0 || len(poster.uploads) != 0 {
		t.Fatalf("response=%#v uploads=%d", response, len(poster.uploads))
	}
	if !strings.Contains(string(response.Result), `"text":"ok"`) {
		t.Fatalf("short text was not kept inline: %s", response.Result)
	}
}

func TestExternalizeMCPResultUploadsLongText(t *testing.T) {
	poster := &deliveryTestPoster{}
	gateway := &Gateway{poster: poster}
	text := strings.Repeat("x", inlineMCPTextBytes+1)
	response := gateway.externalizeMCPResult(context.Background(), slackcore.Message{MessageTS: "1.000"}, completedDeliveryResponse("REQ-LONG", map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
	}))
	if response.Status != protocol.MCPStatusCompleted || len(response.Resources) != 1 || len(poster.uploads) != 1 {
		t.Fatalf("response=%#v uploads=%d", response, len(poster.uploads))
	}
	if string(poster.uploads[0].Data) != text || poster.uploads[0].MediaType != "text/plain" || !strings.HasSuffix(poster.uploads[0].Name, ".txt") {
		t.Fatalf("upload=%#v", poster.uploads[0])
	}
	if poster.threads[0] != "1.000" {
		t.Fatalf("thread=%q", poster.threads[0])
	}
	if !strings.Contains(string(response.Result), "delivered long text") {
		t.Fatalf("result=%s", response.Result)
	}
}

func TestExternalizeMCPResultUploadsImageContent(t *testing.T) {
	poster := &deliveryTestPoster{}
	gateway := &Gateway{poster: poster}
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	response := gateway.externalizeMCPResult(context.Background(), slackcore.Message{MessageTS: "1.000"}, completedDeliveryResponse("REQ-IMAGE", map[string]any{
		"content": []any{map[string]any{
			"type":     "image",
			"mimeType": "image/png",
			"data":     base64.StdEncoding.EncodeToString(png),
		}},
	}))
	if response.Status != protocol.MCPStatusCompleted || len(response.Resources) != 1 || len(poster.uploads) != 1 {
		t.Fatalf("response=%#v uploads=%d", response, len(poster.uploads))
	}
	if string(poster.uploads[0].Data) != string(png) || poster.uploads[0].MediaType != "image/png" || !strings.HasSuffix(poster.uploads[0].Name, ".png") {
		t.Fatalf("upload=%#v", poster.uploads[0])
	}
}

func TestExternalizeMCPResourceReadUploadsReturnedBytesOnly(t *testing.T) {
	poster := &deliveryTestPoster{}
	gateway := &Gateway{poster: poster}
	response := gateway.externalizeMCPResult(context.Background(), slackcore.Message{MessageTS: "1.000"}, completedDeliveryResponse("REQ-RESOURCE", map[string]any{
		"contents": []any{map[string]any{
			"uri":      "file:///repo/main.go",
			"mimeType": "text/plain",
			"text":     "package main\n",
		}},
	}))
	if response.Status != protocol.MCPStatusCompleted || len(response.Resources) != 1 || len(poster.uploads) != 1 {
		t.Fatalf("response=%#v uploads=%d", response, len(poster.uploads))
	}
	if string(poster.uploads[0].Data) != "package main\n" || !strings.Contains(poster.uploads[0].Name, "main.go") {
		t.Fatalf("upload=%#v", poster.uploads[0])
	}
	if strings.Contains(string(response.Result), "package main") || !strings.Contains(string(response.Result), "slack_file") {
		t.Fatalf("resource bytes were not externalized: %s", response.Result)
	}
}

func TestExternalizeMCPResultNeverReadsPathOnlyResource(t *testing.T) {
	poster := &deliveryTestPoster{}
	gateway := &Gateway{poster: poster}
	response := gateway.externalizeMCPResult(context.Background(), slackcore.Message{MessageTS: "1.000"}, completedDeliveryResponse("REQ-PATH", map[string]any{
		"contents": []any{map[string]any{
			"uri":      "file:///definitely/not/read/by/cwapi/secret.txt",
			"mimeType": "text/plain",
		}},
	}))
	if response.Status != protocol.MCPStatusCompleted || len(response.Resources) != 0 || len(poster.uploads) != 0 {
		t.Fatalf("path-only resource triggered outbound access: response=%#v uploads=%d", response, len(poster.uploads))
	}
}

func TestExternalizeMCPResultRejectsOversizedArtifact(t *testing.T) {
	poster := &deliveryTestPoster{}
	gateway := &Gateway{poster: poster}
	response := gateway.externalizeMCPResult(context.Background(), slackcore.Message{MessageTS: "1.000"}, completedDeliveryResponse("REQ-LARGE", map[string]any{
		"contents": []any{map[string]any{
			"uri":      "file:///repo/huge.log",
			"mimeType": "text/plain",
			"text":     strings.Repeat("x", MaxSlackArtifactBytes+1),
		}},
	}))
	if response.Status != protocol.MCPStatusFailed || response.Error == nil || response.Error.Code != "MCP_DELIVERY_FILE_TOO_LARGE" {
		t.Fatalf("response=%#v", response)
	}
	if len(poster.uploads) != 0 {
		t.Fatalf("oversized artifact was uploaded: %d", len(poster.uploads))
	}
}
