package attachments

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPContent intentionally emits native image content only. CWapi no longer
// turns ordinary text/binary attachments into EmbeddedResource file objects on
// the ChatGPT MCP surface.
func MCPContent(item Item, label, _ string) []mcp.Content {
	summary := fmt.Sprintf("CWapi image: %s name=%q mime=%s size=%d sha256=%s", label, item.Metadata.Name, item.Metadata.MIMEType, item.Metadata.Size, item.Metadata.SHA256)
	content := []mcp.Content{&mcp.TextContent{Text: summary}}
	if item.Metadata.Kind != "image" {
		return content
	}
	return append(content, &mcp.ImageContent{Data: item.Data, MIMEType: item.Metadata.MIMEType})
}
