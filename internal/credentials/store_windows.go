//go:build windows

package credentials

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	maxCredentialBlobBytes  = 2560
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type winCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsStore struct{}

func newPlatformStore() secretStore {
	return windowsStore{}
}

func (windowsStore) Read(target string) (string, bool, error) {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", false, fmt.Errorf("credential target: %w", err)
	}
	var credential *winCredential
	result, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&credential)),
	)
	runtime.KeepAlive(targetPtr)
	if result == 0 {
		if isWinErrno(callErr, windows.ERROR_NOT_FOUND) {
			return "", false, nil
		}
		return "", false, winCallError("CredReadW", callErr)
	}
	if credential == nil {
		return "", false, fmt.Errorf("CredReadW returned nil credential")
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential.CredentialBlobSize > maxCredentialBlobBytes {
		return "", false, fmt.Errorf("credential blob too large: %d", credential.CredentialBlobSize)
	}
	if credential.CredentialBlobSize == 0 {
		return "", true, nil
	}
	if credential.CredentialBlob == nil {
		return "", false, fmt.Errorf("credential blob pointer is nil")
	}
	blob := unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))
	copyOfBlob := append([]byte(nil), blob...)
	value, err := decodeCredentialText(copyOfBlob)
	if err != nil {
		return "", false, fmt.Errorf("credential blob decode: %w", err)
	}
	return value, true, nil
}

func (windowsStore) Write(target, value string) error {
	if value == "" {
		return fmt.Errorf("empty credential value")
	}
	blob := encodeCredentialText(value)
	if len(blob) > maxCredentialBlobBytes {
		return fmt.Errorf("credential blob too large: %d", len(blob))
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("credential target: %w", err)
	}
	userPtr, err := windows.UTF16PtrFromString("CWapi")
	if err != nil {
		return fmt.Errorf("credential username: %w", err)
	}
	credential := winCredential{
		Type:               credTypeGeneric,
		TargetName:         targetPtr,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
		UserName:           userPtr,
	}
	result, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&credential)), 0)
	runtime.KeepAlive(targetPtr)
	runtime.KeepAlive(userPtr)
	runtime.KeepAlive(blob)
	if result == 0 {
		return winCallError("CredWriteW", callErr)
	}
	return nil
}

func (windowsStore) Delete(target string) error {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("credential target: %w", err)
	}
	result, _, callErr := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		credTypeGeneric,
		0,
	)
	runtime.KeepAlive(targetPtr)
	if result == 0 {
		if isWinErrno(callErr, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return winCallError("CredDeleteW", callErr)
	}
	return nil
}

func encodeCredentialText(value string) []byte {
	units := utf16.Encode([]rune(value))
	blob := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(blob[i*2:], unit)
	}
	return blob
}

func decodeCredentialText(blob []byte) (string, error) {
	if looksLikeUTF16LEText(blob) {
		units := make([]uint16, len(blob)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(blob[i*2:])
		}
		for len(units) > 0 && units[len(units)-1] == 0 {
			units = units[:len(units)-1]
		}
		return string(utf16.Decode(units)), nil
	}
	if !utf8.Valid(blob) {
		return "", fmt.Errorf("unsupported credential text encoding")
	}
	return string(blob), nil
}

func looksLikeUTF16LEText(blob []byte) bool {
	if len(blob) < 2 || len(blob)%2 != 0 {
		return false
	}
	pairs := len(blob) / 2
	zeroHighBytes := 0
	printableASCIIPairs := 0
	for i := 0; i < len(blob); i += 2 {
		if blob[i+1] == 0 {
			zeroHighBytes++
			if blob[i] >= 0x20 && blob[i] <= 0x7e {
				printableASCIIPairs++
			}
		}
	}
	return printableASCIIPairs > 0 && zeroHighBytes*10 >= pairs*8
}

func isWinErrno(err error, want syscall.Errno) bool {
	errno, ok := err.(syscall.Errno)
	return ok && errno == want
}

func winCallError(operation string, err error) error {
	if err == nil || isWinErrno(err, 0) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
