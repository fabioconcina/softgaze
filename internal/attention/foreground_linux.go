//go:build linux

package attention

import (
	"os"
	"os/exec"
)

// Foreground returns the active window on X11, from xprop. Wayland does not
// let other programs see the active window, so there it is unsupported.
func Foreground() (Window, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") == "" {
		return Window{}, ErrUnsupported
	}
	if _, err := exec.LookPath("xprop"); err != nil {
		return Window{}, ErrUnsupported
	}
	out, err := exec.Command("xprop", "-root", "_NET_ACTIVE_WINDOW").Output()
	if err != nil {
		return Window{}, err
	}
	id := parseActiveWindow(string(out))
	if id == "" {
		return Window{}, nil
	}
	out, err = exec.Command("xprop", "-id", id, "WM_CLASS", "_NET_WM_NAME").Output()
	if err != nil {
		return Window{}, err
	}
	return parseXprop(string(out)), nil
}
