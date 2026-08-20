package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadFileUsesExternalUploadFlow(t *testing.T) {
	var uploaded string
	var completed string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/files.getUploadURLExternal":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("filename") != "result.txt" || request.Form.Get("length") != "7" {
				t.Fatalf("prepare form=%v", request.Form)
			}
			response.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(response, `{"ok":true,"upload_url":%q,"file_id":"F123"}`, server.URL+"/upload")
		case "/upload":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			uploaded = string(body)
			if request.Header.Get("Content-Type") != "text/plain" {
				t.Fatalf("content-type=%q", request.Header.Get("Content-Type"))
			}
			response.WriteHeader(http.StatusOK)
		case "/files.completeUploadExternal":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			completed = request.Form.Get("files")
			if request.Form.Get("channel_id") != "C123456789" || request.Form.Get("thread_ts") != "1.000" {
				t.Fatalf("complete form=%v", request.Form)
			}
			response.Header().Set("Content-Type", "application/json")
			io.WriteString(response, `{"ok":true,"files":[{"id":"F123","name":"result.txt","size":7,"permalink":"https://example.slack.com/files/F123"}]}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := NewClient("xapp-test", "xoxb-test", "C123456789", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.setBaseURLForTest(server.URL)
	file, err := client.UploadFile(context.Background(), "result.txt", "text/plain", []byte("payload"), "1.000")
	if err != nil {
		t.Fatal(err)
	}
	if uploaded != "payload" || !strings.Contains(completed, `"id":"F123"`) {
		t.Fatalf("uploaded=%q completed=%q", uploaded, completed)
	}
	if file.FileID != "F123" || file.Name != "result.txt" || file.Size != 7 || file.Permalink != "https://example.slack.com/files/F123" {
		t.Fatalf("file=%#v", file)
	}
}

func TestValidateExternalUploadURLRejectsPlainHTTPRemoteHost(t *testing.T) {
	if err := validateExternalUploadURL("http://example.com/upload"); err == nil {
		t.Fatal("plain HTTP remote upload URL was accepted")
	}
	if err := validateExternalUploadURL("https://files.slack.com/upload"); err != nil {
		t.Fatalf("HTTPS upload URL rejected: %v", err)
	}
}
