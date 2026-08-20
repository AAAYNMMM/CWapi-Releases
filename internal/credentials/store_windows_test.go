//go:build windows

package credentials

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestWindowsCredentialManagerRoundTrip(t *testing.T) {
	store := windowsStore{}
	target := fmt.Sprintf("CWapi/v1.6.0/Test/%d/%d", os.Getpid(), time.Now().UnixNano())
	defer store.Delete(target)

	if err := store.Write(target, "cwapi-test-secret"); err != nil {
		t.Fatal(err)
	}
	value, present, err := store.Read(target)
	if err != nil {
		t.Fatal(err)
	}
	if !present || value != "cwapi-test-secret" {
		t.Fatalf("credential roundtrip: present=%v value=%q", present, value)
	}
	if err := store.Delete(target); err != nil {
		t.Fatal(err)
	}
	_, present, err = store.Read(target)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("credential remained after delete")
	}
}

func TestCredentialTextEncodingUsesUTF16LE(t *testing.T) {
	blob := encodeCredentialText("xapp-test-token")
	if len(blob) != len("xapp-test-token")*2 {
		t.Fatalf("encoded length = %d", len(blob))
	}
	if !looksLikeUTF16LEText(blob) {
		t.Fatal("encoded token was not recognized as UTF-16LE text")
	}
	value, err := decodeCredentialText(blob)
	if err != nil {
		t.Fatal(err)
	}
	if value != "xapp-test-token" {
		t.Fatalf("decoded value = %q", value)
	}
}

func TestCredentialTextDecoderAcceptsRawUTF8(t *testing.T) {
	value, err := decodeCredentialText([]byte("xoxb-test-token"))
	if err != nil {
		t.Fatal(err)
	}
	if value != "xoxb-test-token" {
		t.Fatalf("decoded value = %q", value)
	}
}
