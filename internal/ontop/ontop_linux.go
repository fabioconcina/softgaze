//go:build linux

package ontop

import (
	"os"
	"os/exec"
)

// Supported reports whether Pin can work here: an X11 session with wmctrl.
// Wayland does not let one program raise another's window.
func Supported() bool {
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") == "" {
		return false
	}
	_, err := exec.LookPath("wmctrl")
	return err == nil
}

// Focus is not done here on Linux: the caller asks the editor's CLI.
func Focus(func(app, title string) bool) error { return ErrUnsupported }

// Foreground is zero: softgaze does not track windows here.
func Foreground() Window { return 0 }

// Activate and App do nothing without a window.
func (Window) Activate() bool { return false }
func (Window) App() string    { return "" }

// Pin keeps the window whose title contains token above the others.
func Pin(token string) error {
	if !Supported() {
		return ErrUnsupported
	}
	if err := exec.Command("wmctrl", "-r", token, "-b", "add,above").Run(); err != nil {
		return ErrNotFound // wmctrl fails when no title matches
	}
	return nil
}
