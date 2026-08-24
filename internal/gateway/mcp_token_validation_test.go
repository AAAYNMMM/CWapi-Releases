package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

func TestInvalidSystemTokenIsRejectedBeforeClaim(t *testing.T) {
	token := strings.Repeat("A", 64)
	for _, test := range []struct {
		name, tokenJSON string
	}{
		{"uppercase", `"system_token":"` + token + `",`},
		{"wrong-type", `"system_token":42,`},
		{"null", `"system_token":null,`},
		{"nested", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway, store, poster := newFeatureGateway(t, nil)
			requestID := "REQTOKEN" + strings.ToUpper(test.name[:1])
			params := `{"server":"cwapi","tool":"process_start","arguments":{"command":"node"}}`
			if test.name == "nested" {
				params = `{"server":"cwapi","tool":"process_start","arguments":{"command":"node","system_token":"` + strings.Repeat("a", 64) + `"}}`
			}
			body := `{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"` + requestID +
				`","repository_url":"https://github.com/o/r","expected_commit":"` + strings.Repeat("a", 40) +
				`","method":"mcpServer/tool/call",` + test.tokenJSON + `"params":` + params + `}`
			subject, err := protocol.BuildMCPSubject(protocol.MCPFamilyRequest, requestID)
			if err != nil {
				t.Fatal(err)
			}
			message := slackcore.Message{MessageID: "m", ChannelID: "C12345678", MessageTS: "1", Subject: subject, Body: body}
			if err := gateway.HandleMCPMessage(context.Background(), message); err != nil {
				t.Fatal(err)
			}
			response := decodeFeatureResponse(t, poster)
			if response.Error == nil || response.Error.Code != "MCP_SYSTEM_TOKEN_INVALID" {
				t.Fatalf("response=%#v", response)
			}
			if _, err := store.MCPRequestByID(context.Background(), requestID); err == nil {
				t.Fatal("invalid token claimed request_id")
			}
		})
	}
}
