//go:build windows

package attention

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetWindowTextW = windows.NewLazySystemDLL("user32.dll").NewProc("GetWindowTextW")

// Foreground returns the window in front: its title and the executable that
// owns it. No permission is needed.
func Foreground() (Window, error) {
	h := windows.GetForegroundWindow()
	if h == 0 {
		return Window{}, nil // switching windows, or the desktop
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	w := Window{Title: windows.UTF16ToString(buf[:n])}

	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(h, &pid); err == nil && pid != 0 {
		// Fails for elevated processes; the title still decides.
		if p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
			path := make([]uint16, windows.MAX_PATH)
			size := uint32(len(path))
			if windows.QueryFullProcessImageName(p, 0, &path[0], &size) == nil {
				w.App = filepath.Base(windows.UTF16ToString(path[:size]))
			}
			windows.CloseHandle(p)
		}
	}
	return w, nil
}
