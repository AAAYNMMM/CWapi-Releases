package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const mib = 1024 * 1024

type Policy struct {
	MaxFiles           int
	MaxAttachmentBytes int64
	MaxBatchBytes      int64
	MaxImageSide       int
}

func CodingPolicy() Policy {
	return Policy{MaxFiles: 16, MaxAttachmentBytes: 32 * mib, MaxBatchBytes: 64 * mib, MaxImageSide: 4096}
}

func AgentPolicy() Policy {
	return Policy{MaxFiles: 8, MaxAttachmentBytes: 8 * mib, MaxBatchBytes: 16 * mib, MaxImageSide: 2048}
}

type Metadata struct {
	Name     string `json:"name"`
	Ref      string `json:"ref,omitempty"`
	MIMEType string `json:"mime_type"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type Item struct {
	Metadata Metadata `json:"metadata"`
	Data     []byte   `json:"-"`
}

type Batch struct {
	Items      []Item `json:"items"`
	TotalBytes int64  `json:"total_bytes"`
}

type InlineInput struct {
	Name       string `json:"name"`
	MIMEType   string `json:"mime_type,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
	DataURI    string `json:"data_uri,omitempty"`
	Text       string `json:"text,omitempty"`
}

func LoadWorkspace(ctx context.Context, root string, paths []string, policy Policy) (Batch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validatePolicy(policy); err != nil {
		return Batch{}, err
	}
	if len(paths) == 0 || len(paths) > policy.MaxFiles {
		return Batch{}, errors.New("ATTACHMENT_FILE_COUNT_INVALID")
	}
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return Batch{}, errors.New("ATTACHMENT_WORKSPACE_ROOT_INVALID")
	}
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return Batch{}, fmt.Errorf("ATTACHMENT_WORKSPACE_ROOT_RESOLVE_FAILED: %w", err)
	}
	var batch Batch
	batch.Items = make([]Item, 0, len(paths))
	for _, raw := range paths {
		select {
		case <-ctx.Done():
			return Batch{}, ctx.Err()
		default:
		}
		relative, candidate, err := resolveWorkspacePath(resolvedRoot, raw)
		if err != nil {
			return Batch{}, err
		}
		info, err := os.Stat(candidate)
		if err != nil {
			return Batch{}, fmt.Errorf("ATTACHMENT_FILE_STAT_FAILED: %w", err)
		}
		if !info.Mode().IsRegular() {
			return Batch{}, errors.New("ATTACHMENT_FILE_NOT_REGULAR")
		}
		if info.Size() < 0 || info.Size() > policy.MaxAttachmentBytes {
			return Batch{}, errors.New("ATTACHMENT_FILE_TOO_LARGE")
		}
		data, err := readBounded(candidate, policy.MaxAttachmentBytes)
		if err != nil {
			return Batch{}, err
		}
		item, err := buildItem(relative, filepath.Base(candidate), "", data, policy)
		if err != nil {
			return Batch{}, err
		}
		if batch.TotalBytes+item.Metadata.Size > policy.MaxBatchBytes {
			return Batch{}, errors.New("ATTACHMENT_BATCH_TOO_LARGE")
		}
		batch.TotalBytes += item.Metadata.Size
		batch.Items = append(batch.Items, item)
	}
	return batch, nil
}

func DecodeInline(inputs []InlineInput, policy Policy) (Batch, error) {
	if err := validatePolicy(policy); err != nil {
		return Batch{}, err
	}
	if len(inputs) == 0 || len(inputs) > policy.MaxFiles {
		return Batch{}, errors.New("ATTACHMENT_FILE_COUNT_INVALID")
	}
	batch := Batch{Items: make([]Item, 0, len(inputs))}
	for index, input := range inputs {
		data, declaredMIME, err := decodeInlineData(input)
		if err != nil {
			return Batch{}, err
		}
		name := SanitizeFilename(input.Name)
		if strings.TrimSpace(input.Name) == "" {
			name = fmt.Sprintf("attachment-%02d", index+1)
		}
		mimeType := strings.TrimSpace(input.MIMEType)
		if mimeType == "" {
			mimeType = declaredMIME
		}
		item, err := buildItem("", name, mimeType, data, policy)
		if err != nil {
			return Batch{}, err
		}
		if batch.TotalBytes+item.Metadata.Size > policy.MaxBatchBytes {
			return Batch{}, errors.New("ATTACHMENT_BATCH_TOO_LARGE")
		}
		batch.TotalBytes += item.Metadata.Size
		batch.Items = append(batch.Items, item)
	}
	return batch, nil
}

func SanitizeFilename(value string) string {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	if value == "" || value == "." || value == ".." {
		return "attachment.bin"
	}
	var builder strings.Builder
	for _, r := range value {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			builder.WriteRune('_')
		} else {
			builder.WriteRune(r)
		}
		if builder.Len() >= 120 {
			break
		}
	}
	result := strings.Trim(builder.String(), " .")
	if result == "" {
		return "attachment.bin"
	}
	return result
}

func validatePolicy(policy Policy) error {
	if policy.MaxFiles <= 0 || policy.MaxAttachmentBytes <= 0 || policy.MaxBatchBytes <= 0 || policy.MaxAttachmentBytes > policy.MaxBatchBytes {
		return errors.New("ATTACHMENT_POLICY_INVALID")
	}
	return nil
}

func resolveWorkspacePath(root, raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", "", errors.New("ATTACHMENT_PATH_INVALID")
	}
	relative := filepath.Clean(filepath.FromSlash(raw))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("ATTACHMENT_PATH_OUTSIDE_WORKSPACE")
	}
	for _, component := range strings.Split(filepath.ToSlash(relative), "/") {
		if strings.EqualFold(component, ".git") {
			return "", "", errors.New("ATTACHMENT_GIT_METADATA_FORBIDDEN")
		}
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, relative))
	if err != nil {
		return "", "", fmt.Errorf("ATTACHMENT_PATH_RESOLVE_FAILED: %w", err)
	}
	if !underRoot(root, candidate) {
		return "", "", errors.New("ATTACHMENT_PATH_OUTSIDE_WORKSPACE")
	}
	return filepath.ToSlash(relative), candidate, nil
}

func underRoot(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ATTACHMENT_FILE_OPEN_FAILED: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("ATTACHMENT_FILE_READ_FAILED: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("ATTACHMENT_FILE_TOO_LARGE")
	}
	return data, nil
}

func decodeInlineData(input InlineInput) ([]byte, string, error) {
	sources := 0
	if input.DataBase64 != "" {
		sources++
	}
	if input.DataURI != "" {
		sources++
	}
	if input.Text != "" {
		sources++
	}
	if sources != 1 {
		return nil, "", errors.New("ATTACHMENT_INLINE_DATA_INVALID")
	}
	if input.Text != "" {
		return []byte(input.Text), "text/plain", nil
	}
	if input.DataBase64 != "" {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(input.DataBase64))
		if err != nil {
			return nil, "", errors.New("ATTACHMENT_BASE64_INVALID")
		}
		return data, "", nil
	}
	return decodeDataURI(input.DataURI)
}

func decodeDataURI(value string) ([]byte, string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return nil, "", errors.New("ATTACHMENT_DATA_URI_INVALID")
	}
	header, payload, ok := strings.Cut(value[5:], ",")
	if !ok {
		return nil, "", errors.New("ATTACHMENT_DATA_URI_INVALID")
	}
	parts := strings.Split(header, ";")
	mimeType := strings.TrimSpace(parts[0])
	encoded := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			encoded = true
		}
	}
	if encoded {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return nil, "", errors.New("ATTACHMENT_BASE64_INVALID")
		}
		return data, mimeType, nil
	}
	decoded, err := url.PathUnescape(payload)
	if err != nil {
		return nil, "", errors.New("ATTACHMENT_DATA_URI_INVALID")
	}
	return []byte(decoded), mimeType, nil
}

func buildItem(ref, name, providedMIME string, data []byte, policy Policy) (Item, error) {
	if int64(len(data)) > policy.MaxAttachmentBytes {
		return Item{}, errors.New("ATTACHMENT_FILE_TOO_LARGE")
	}
	name = SanitizeFilename(name)
	mimeType, err := detectMIME(name, providedMIME, data)
	if err != nil {
		return Item{}, err
	}
	kind := "binary"
	if strings.HasPrefix(mimeType, "image/") {
		kind = "image"
		if err := validateImage(data, mimeType, policy.MaxImageSide); err != nil {
			return Item{}, err
		}
	} else if isTextMIME(mimeType) && utf8.Valid(data) {
		kind = "text"
	}
	digest := sha256.Sum256(data)
	return Item{Metadata: Metadata{
		Name: name, Ref: ref, MIMEType: mimeType, Kind: kind, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]),
	}, Data: data}, nil
}

func detectMIME(name, provided string, data []byte) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		parsed, _, err := mime.ParseMediaType(provided)
		if err != nil || !strings.Contains(parsed, "/") {
			return "", errors.New("ATTACHMENT_MIME_INVALID")
		}
		provided = strings.ToLower(parsed)
	}
	detected := strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
	if parsed, _, err := mime.ParseMediaType(detected); err == nil {
		detected = strings.ToLower(parsed)
	}
	detectedImage := ""
	if strings.HasPrefix(detected, "image/") {
		detectedImage = detected
	} else if isWebP(data) {
		detectedImage = "image/webp"
	}
	if provided != "" && provided != "image/svg+xml" {
		if (detectedImage != "" && provided != detectedImage) || (detectedImage == "" && strings.HasPrefix(provided, "image/")) {
			return "", errors.New("ATTACHMENT_IMAGE_MIME_MISMATCH")
		}
	}
	if provided != "" {
		return provided, nil
	}
	if detectedImage != "" {
		return detectedImage, nil
	}
	if byExt := MIMEByName(name); byExt != "" {
		return byExt, nil
	}
	if detected == "" {
		return "application/octet-stream", nil
	}
	return detected, nil
}

func MIMEByName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json"
	case ".md", ".markdown":
		return "text/markdown"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".toml":
		return "application/toml"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	value := mime.TypeByExtension(filepath.Ext(name))
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed)
}

func isTextMIME(value string) bool {
	if strings.HasPrefix(value, "text/") {
		return true
	}
	switch value {
	case "application/json", "application/xml", "application/yaml", "application/toml", "application/javascript", "application/x-javascript":
		return true
	default:
		return false
	}
}

func validateImage(data []byte, mimeType string, maxSide int) error {
	if mimeType == "image/svg+xml" {
		return errors.New("ATTACHMENT_IMAGE_FORMAT_UNSUPPORTED")
	}
	if maxSide <= 0 {
		return nil
	}
	width, height := 0, 0
	if mimeType == "image/webp" {
		var err error
		width, height, err = webPDimensions(data)
		if err != nil {
			return err
		}
	} else {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return errors.New("ATTACHMENT_IMAGE_INVALID")
		}
		width, height = config.Width, config.Height
	}
	if width <= 0 || height <= 0 || width > maxSide || height > maxSide {
		return errors.New("ATTACHMENT_IMAGE_DIMENSIONS_EXCEEDED")
	}
	return nil
}

func isWebP(data []byte) bool {
	return len(data) >= 16 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

func webPDimensions(data []byte) (int, int, error) {
	if !isWebP(data) || len(data) < 20 {
		return 0, 0, errors.New("ATTACHMENT_IMAGE_INVALID")
	}
	riffEnd := uint64(binary.LittleEndian.Uint32(data[4:8])) + 8
	chunkSize := uint64(binary.LittleEndian.Uint32(data[16:20]))
	chunkEnd := uint64(20) + chunkSize
	paddedChunkEnd := chunkEnd + chunkSize%2
	if riffEnd < 20 || riffEnd > uint64(len(data)) || paddedChunkEnd > riffEnd || paddedChunkEnd > uint64(len(data)) {
		return 0, 0, errors.New("ATTACHMENT_IMAGE_INVALID")
	}
	payload := data[20:chunkEnd]
	switch string(data[12:16]) {
	case "VP8X":
		if len(payload) < 10 {
			return 0, 0, errors.New("ATTACHMENT_IMAGE_INVALID")
		}
		width := 1 + int(payload[4]) + int(payload[5])<<8 + int(payload[6])<<16
		height := 1 + int(payload[7]) + int(payload[8])<<8 + int(payload[9])<<16
		return width, height, nil
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			return 0, 0, errors.New("ATTACHMENT_IMAGE_INVALID")
		}
		width := 1 + int(payload[1]) + int(payload[2]&0x3f)<<8
		height := 1 + int(payload[2]>>6) + int(payload[3])<<2 + int(payload[4]&0x0f)<<10
		return width, height, nil
	case "VP8 ":
		if len(payload) < 10 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
			return 0, 0, errors.New("ATTACHMENT_IMAGE_INVALID")
		}
		width := int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
		return width, height, nil
	default:
		return 0, 0, errors.New("ATTACHMENT_IMAGE_INVALID")
	}
}
