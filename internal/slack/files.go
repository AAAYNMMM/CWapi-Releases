package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const maxUploadResponseBytes = 64 * 1024

type FileUpload struct {
	FileID    string `json:"file_id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Permalink string `json:"permalink,omitempty"`
}

// UploadFile uses Slack's external upload flow. CWapi's product-level size and
// content policy is enforced by the caller before bytes reach this transport.
func (c *Client) UploadFile(ctx context.Context, filename, mediaType string, data []byte, threadTS string) (FileUpload, error) {
	filename = strings.TrimSpace(filepath.Base(filename))
	if filename == "" || filename == "." || filename == string(filepath.Separator) || strings.ContainsAny(filename, "\r\n") {
		return FileUpload{}, errors.New("SLACK_FILE_NAME_INVALID")
	}
	if len(data) == 0 {
		return FileUpload{}, errors.New("SLACK_FILE_EMPTY")
	}
	if mediaType = strings.TrimSpace(mediaType); mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if strings.ContainsAny(mediaType, "\r\n") {
		return FileUpload{}, errors.New("SLACK_FILE_MEDIA_TYPE_INVALID")
	}

	form := url.Values{}
	form.Set("filename", filename)
	form.Set("length", strconv.Itoa(len(data)))
	var prepare struct {
		slackResponse
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := c.doForm(ctx, c.botToken, "files.getUploadURLExternal", form, &prepare); err != nil {
		return FileUpload{}, err
	}
	uploadURL := strings.TrimSpace(prepare.UploadURL)
	fileID := strings.TrimSpace(prepare.FileID)
	if fileID == "" {
		return FileUpload{}, errors.New("SLACK_FILE_UPLOAD_ID_MISSING")
	}
	if err := validateExternalUploadURL(uploadURL); err != nil {
		return FileUpload{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(data))
	if err != nil {
		return FileUpload{}, fmt.Errorf("SLACK_FILE_UPLOAD_REQUEST_CREATE_FAILED: %w", err)
	}
	request.Header.Set("Content-Type", mediaType)
	response, err := c.http.Do(request)
	if err != nil {
		return FileUpload{}, fmt.Errorf("SLACK_FILE_UPLOAD_FAILED: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return FileUpload{}, fmt.Errorf("SLACK_FILE_UPLOAD_HTTP_STATUS_%d", response.StatusCode)
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxUploadResponseBytes)); err != nil {
		return FileUpload{}, errors.New("SLACK_FILE_UPLOAD_RESPONSE_READ_FAILED")
	}

	filesJSON, err := json.Marshal([]map[string]string{{"id": fileID, "title": filename}})
	if err != nil {
		return FileUpload{}, fmt.Errorf("SLACK_FILE_COMPLETE_ENCODE_FAILED: %w", err)
	}
	completeForm := url.Values{}
	completeForm.Set("files", string(filesJSON))
	completeForm.Set("channel_id", c.channelID)
	threadTS = strings.TrimSpace(threadTS)
	if threadTS != "" {
		completeForm.Set("thread_ts", threadTS)
	}
	var complete struct {
		slackResponse
		Files []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Size      int64  `json:"size"`
			Permalink string `json:"permalink"`
		} `json:"files"`
	}
	if err := c.doForm(ctx, c.botToken, "files.completeUploadExternal", completeForm, &complete); err != nil {
		return FileUpload{}, err
	}
	result := FileUpload{FileID: fileID, Name: filename, Size: int64(len(data))}
	for _, file := range complete.Files {
		if strings.TrimSpace(file.ID) != fileID {
			continue
		}
		if name := strings.TrimSpace(file.Name); name != "" {
			result.Name = name
		}
		if file.Size > 0 {
			result.Size = file.Size
		}
		result.Permalink = strings.TrimSpace(file.Permalink)
		break
	}
	return result, nil
}

func validateExternalUploadURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return errors.New("SLACK_FILE_UPLOAD_URL_INVALID")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			return nil
		}
	}
	return errors.New("SLACK_FILE_UPLOAD_URL_NOT_TLS")
}
