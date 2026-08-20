//go:build windows

package tray

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// loadTrayIcon uses the icon embedded by Wails in CWapi.exe. The system icon is
// only a fallback for development test binaries that do not carry resources.
func loadTrayIcon() (uintptr, bool) {
	if executable, err := os.Executable(); err == nil {
		if icon, owned := loadExecutableIcon(executable); icon != 0 {
			return icon, owned
		}
	}
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	return icon, false
}

func loadExecutableIcon(executable string) (uintptr, bool) {
	path, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return 0, false
	}
	var large, small uintptr
	count, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(path)), 0,
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1,
	)
	if count == 0 {
		return 0, false
	}
	if small != 0 {
		if large != 0 {
			procDestroyIcon.Call(large)
		}
		return small, true
	}
	return large, large != 0
}
