//go:build windows

package ontop

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procShowWindow          = user32.NewProc("ShowWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procSwitchToThisWindow  = user32.NewProc("SwitchToThisWindow")
	procSendInput           = user32.NewProc("SendInput")
	procGetWindowLongW      = user32.NewProc("GetWindowLongW")
	procIsWindow            = user32.NewProc("IsWindow")
)

const (
	hwndTopmost   = ^uintptr(0) // HWND_TOPMOST, (HWND)-1
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swRestore     = 9
	gwlExStyle    = ^uintptr(19) // GWL_EXSTYLE, -20
	wsExTopmost   = 0x0008
)

type topWindow struct {
	h     windows.HWND
	title string
}

// One callback for the process lifetime: Windows callbacks are a limited
// resource, so the listing is shared under a lock.
var (
	enumMu   sync.Mutex
	enumList []topWindow
	enumCB   = syscall.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		if windows.IsWindowVisible(h) {
			buf := make([]uint16, 512)
			n, _, _ := procGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if n > 0 {
				enumList = append(enumList, topWindow{h, windows.UTF16ToString(buf[:n])})
			}
		}
		return 1
	})
)

// visibleWindows lists the visible top-level windows that have a title.
func visibleWindows() []topWindow {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumList = nil
	_ = windows.EnumWindows(enumCB, unsafe.Pointer(nil))
	return enumList
}

func exeOf(h windows.HWND) string {
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(h, &pid); err != nil || pid == 0 {
		return ""
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(p)
	path := make([]uint16, windows.MAX_PATH)
	size := uint32(len(path))
	if windows.QueryFullProcessImageName(p, 0, &path[0], &size) != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(path[:size]))
}

// Supported reports whether Pin can work here.
func Supported() bool { return true }

// Pin keeps the visible top-level window whose title contains token above
// all other windows (the same as PowerToys' Always On Top), without moving,
// resizing or focusing it.
func Pin(token string) error {
	for _, w := range visibleWindows() {
		if strings.Contains(w.title, token) {
			if r, _, err := procSetWindowPos.Call(uintptr(w.h), hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate); r == 0 {
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

// Focus brings to the front the first visible window that match accepts
// (given its executable and title), restoring it if minimized. It skips
// the window in front, the one the user asked from (Softgaze itself, which
// may be a VS Code window named after the same project), and windows kept
// on top (a browser's floating Softgaze window).
func Focus(match func(app, title string) bool) error {
	from := windows.GetForegroundWindow()
	for _, w := range visibleWindows() {
		if w.h == from || isTopmost(w.h) {
			continue
		}
		if match(exeOf(w.h), w.title) {
			if !activate(w.h) {
				return ErrRefused
			}
			return nil
		}
	}
	return ErrNotFound
}

// Foreground is the window in front.
func Foreground() Window { return Window(windows.GetForegroundWindow()) }

func (a Window) exists() bool {
	if a == 0 {
		return false
	}
	r, _, _ := procIsWindow.Call(uintptr(a))
	return r != 0
}

// App is the window's executable ("" when it is gone).
func (a Window) App() string {
	if !a.exists() {
		return ""
	}
	return exeOf(windows.HWND(a))
}

// Activate restores the window if minimized and brings it to the front
// with the keyboard, reporting whether it got there.
func (a Window) Activate() bool { return a.exists() && activate(windows.HWND(a)) }

func isTopmost(h windows.HWND) bool {
	ex, _, _ := procGetWindowLongW.Call(uintptr(h), gwlExStyle)
	return ex&wsExTopmost != 0
}

// mouseInput is Windows' INPUT with a MOUSEINPUT (type 0, INPUT_MOUSE): the
// inner struct's alignment puts it at the offset C does on 32 and 64 bits.
type mouseInput struct {
	typ uint32
	mi  struct {
		dx, dy                 int32
		mouseData, flags, time uint32
		extra                  uintptr
	}
}

// activate brings h to the front and reports whether it got there. Windows
// lets only the app that had the last input change the foreground window,
// so softgaze first sends an empty mouse event (no movement, no button: the
// same trick as PowerToys' window switcher) and briefly shares input with
// the foreground app (the browser the user just clicked).
func activate(h windows.HWND) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := procIsIconic.Call(uintptr(h)); r != 0 {
		procShowWindow.Call(uintptr(h), swRestore)
	}
	var in mouseInput
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	cur := windows.GetCurrentThreadId()
	var pid uint32
	if fg := windows.GetForegroundWindow(); fg != 0 {
		if t, err := windows.GetWindowThreadProcessId(fg, &pid); err == nil && t != cur {
			procAttachThreadInput.Call(uintptr(cur), uintptr(t), 1)
			defer procAttachThreadInput.Call(uintptr(cur), uintptr(t), 0)
		}
	}
	procBringWindowToTop.Call(uintptr(h))
	if r, _, _ := procSetForegroundWindow.Call(uintptr(h)); r == 0 {
		procSwitchToThisWindow.Call(uintptr(h), 1)
	}
	return windows.GetForegroundWindow() == h
}
