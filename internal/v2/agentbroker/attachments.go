package agentbroker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/v2/attachments"
)

type attachmentReplacement struct {
	inputIndex int
	part       map[string]any
}

// extractRequestAttachments supports only inline raster images carried by the
// standard Chat Completions image_url data-URI form. CWapi deliberately does
// not accept a generic top-level file attachment extension and does not turn
// ordinary files into MCP EmbeddedResource objects.
func extractRequestAttachments(payload []byte) ([]byte, attachments.Batch, error) {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, attachments.Batch{}, errors.New("AGENT_REQUEST_JSON_INVALID")
	}
	if _, present := root["attachments"]; present {
		return nil, attachments.Batch{}, errors.New("AGENT_FILE_ATTACHMENTS_UNSUPPORTED")
	}

	inputs := make([]attachments.InlineInput, 0)
	replacements := make([]attachmentReplacement, 0)
	if messages, ok := root["messages"].([]any); ok {
		for messageIndex, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			parts, ok := message["content"].([]any)
			if !ok {
				continue
			}
			imageIndex := 0
			for _, rawPart := range parts {
				part, ok := rawPart.(map[string]any)
				if !ok || strings.TrimSpace(stringValue(part["type"])) != "image_url" {
					continue
				}
				image, ok := part["image_url"].(map[string]any)
				if !ok {
					continue
				}
				dataURI := strings.TrimSpace(stringValue(image["url"]))
				if !strings.HasPrefix(strings.ToLower(dataURI), "data:") {
					continue
				}
				imageIndex++
				inputIndex := len(inputs)
				inputs = append(inputs, attachments.InlineInput{
					Name:    fmt.Sprintf("message-%02d-image-%02d", messageIndex+1, imageIndex),
					DataURI: dataURI,
				})
				replacements = append(replacements, attachmentReplacement{inputIndex: inputIndex, part: part})
			}
		}
	}

	if len(inputs) == 0 {
		return payload, attachments.Batch{}, nil
	}
	batch, err := attachments.DecodeInline(inputs, attachments.AgentPolicy())
	if err != nil {
		return nil, attachments.Batch{}, err
	}
	for _, item := range batch.Items {
		if item.Metadata.Kind != "image" {
			return nil, attachments.Batch{}, errors.New("AGENT_IMAGE_ATTACHMENT_REQUIRED")
		}
	}
	for _, replacement := range replacements {
		if replacement.inputIndex < 0 || replacement.inputIndex >= len(batch.Items) {
			return nil, attachments.Batch{}, errors.New("AGENT_ATTACHMENT_MAPPING_INVALID")
		}
		item := batch.Items[replacement.inputIndex]
		for key := range replacement.part {
			delete(replacement.part, key)
		}
		replacement.part["type"] = "text"
		replacement.part["text"] = fmt.Sprintf(
			"[CWapi image: %s; mime=%s; size=%d; sha256=%s]",
			item.Metadata.Name, item.Metadata.MIMEType, item.Metadata.Size, item.Metadata.SHA256,
		)
	}
	sanitized, err := json.Marshal(root)
	if err != nil {
		return nil, attachments.Batch{}, errors.New("AGENT_REQUEST_JSON_INVALID")
	}
	return sanitized, batch, nil
}
