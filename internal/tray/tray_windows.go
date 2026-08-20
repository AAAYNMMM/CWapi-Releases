//go:build windows

package tray

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmClose        = 0x0010
	wmDestroy      = 0x0002
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmTrayIcon     = 0x8001
	idiApplication = 32512

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	trayCommandOpen = 1001
	trayCommandExit = 1002
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	shell32                 = windows.NewLazySystemDLL("shell32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW      = shell32.NewProc("ExtractIconExW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

type point struct {
	X int32
	Y int32
}

type message struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          windows.GUID
	HBalloonIcon      uintptr
}

type windowsImplementation struct {
	mu      sync.Mutex
	started bool
	hwnd    uintptr
	wndProc uintptr
	ready   chan error
	done    chan struct{}
	onOpen  func()
	onExit  func()
}

func newImplementation(onOpen, onExit func()) implementation {
	return &windowsImplementation{onOpen: onOpen, onExit: onExit}
}

func (w *windowsImplementation) Start() error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return nil
	}
	w.started = true
	w.ready = make(chan error, 1)
	w.done = make(chan struct{})
	ready := w.ready
	w.mu.Unlock()

	go w.run()
	if err := <-ready; err != nil {
		w.mu.Lock()
		w.started = false
		w.mu.Unlock()
		return err
	}
	return nil
}

func (w *windowsImplementation) Close() error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	hwnd := w.hwnd
	done := w.done
	w.mu.Unlock()
	if hwnd == 0 {
		return nil
	}
	result, _, callErr := procPostMessageW.Call(hwnd, wmClose, 0, 0)
	if result == 0 {
		return winCallError("PostMessageW", callErr)
	}
	select {
	case <-done:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("TRAY_CLOSE_TIMEOUT")
	}
}

func (w *windowsImplementation) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		w.mu.Lock()
		w.hwnd = 0
		w.started = false
		done := w.done
		w.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	signalReady := func(err error) {
		w.mu.Lock()
		ready := w.ready
		w.ready = nil
		w.mu.Unlock()
		if ready != nil {
			ready <- err
			close(ready)
		}
	}

	hInstance, _, callErr := procGetModuleHandleW.Call(0)
	if hInstance == 0 {
		signalReady(winCallError("GetModuleHandleW", callErr))
		return
	}
	className, err := windows.UTF16PtrFromString(fmt.Sprintf("CWapiTrayWindow-%d", os.Getpid()))
	if err != nil {
		signalReady(fmt.Errorf("TRAY_CLASS_NAME_INVALID: %w", err))
		return
	}
	windowName, err := windows.UTF16PtrFromString("CWapi Tray")
	if err != nil {
		signalReady(fmt.Errorf("TRAY_WINDOW_NAME_INVALID: %w", err))
		return
	}
	w.wndProc = windows.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		return w.windowProc(hwnd, msg, wParam, lParam)
	})
	icon, ownedIcon := loadTrayIcon()
	if icon == 0 {
		signalReady(fmt.Errorf("TRAY_ICON_LOAD_FAILED"))
		return
	}
	if ownedIcon {
		defer procDestroyIcon.Call(icon)
	}
	class := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   w.wndProc,
		HInstance:     hInstance,
		HIcon:         icon,
		LpszClassName: className,
		HIconSm:       icon,
	}
	atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		signalReady(winCallError("RegisterClassExW", callErr))
		return
	}
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		signalReady(winCallError("CreateWindowExW", callErr))
		return
	}
	w.mu.Lock()
	w.hwnd = hwnd
	w.mu.Unlock()

	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayIcon,
		HIcon:            icon,
	}
	setUTF16(nid.SzTip[:], "CWapi v1.6.0")
	added, _, callErr := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if added == 0 {
		_ = w.destroyWindow(hwnd)
		signalReady(winCallError("Shell_NotifyIconW(NIM_ADD)", callErr))
		return
	}
	defer procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	signalReady(nil)

	var msg message
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 || result == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (w *windowsImplementation) windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmLButtonUp:
			go safeInvoke(w.onOpen)
		case wmRButtonUp:
			w.showMenu(hwnd)
		}
		return 0
	case wmClose:
		_ = w.destroyWindow(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	default:
		result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return result
	}
}

func (w *windowsImplementation) showMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	openText, _ := windows.UTF16PtrFromString("打开 CWapi")
	exitText, _ := windows.UTF16PtrFromString("退出 CWapi")
	if ok, _, _ := procAppendMenuW.Call(menu, mfString, trayCommandOpen, uintptr(unsafe.Pointer(openText))); ok == 0 {
		return
	}
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	if ok, _, _ := procAppendMenuW.Call(menu, mfString, trayCommandExit, uintptr(unsafe.Pointer(exitText))); ok == 0 {
		return
	}
	var cursor point
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor))); ok == 0 {
		return
	}
	procSetForegroundWindow.Call(hwnd)
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightButton|tpmNoNotify|tpmReturnCmd,
		uintptr(cursor.X), uintptr(cursor.Y),
		0, hwnd, 0,
	)
	switch command {
	case trayCommandOpen:
		go safeInvoke(w.onOpen)
	case trayCommandExit:
		go safeInvoke(w.onExit)
	}
}

func (w *windowsImplementation) destroyWindow(hwnd uintptr) error {
	result, _, callErr := procDestroyWindow.Call(hwnd)
	if result == 0 {
		return winCallError("DestroyWindow", callErr)
	}
	return nil
}

func setUTF16(destination []uint16, value string) {
	encoded, err := windows.UTF16FromString(value)
	if err != nil {
		return
	}
	copy(destination, encoded)
}

func safeInvoke(callback func()) {
	if callback == nil {
		return
	}
	defer func() { _ = recover() }()
	callback()
}

func winCallError(operation string, err error) error {
	if err == nil || err == windows.ERROR_SUCCESS {
		return fmt.Errorf("TRAY_%s_FAILED", operation)
	}
	return fmt.Errorf("TRAY_%s_FAILED: %w", operation, err)
}
