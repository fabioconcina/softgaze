//go:build !windows && !linux

package ontop

// Supported is false: macOS needs the Accessibility permission to move
// another program's window, and softgaze does not ask for it.
func Supported() bool { return false }

// Focus is not done here: the caller asks the editor's CLI.
func Focus(func(app, title string) bool) error { return ErrUnsupported }

// Foreground is zero: softgaze does not track windows here.
func Foreground() Window { return 0 }

// Activate and App do nothing without a window.
func (Window) Activate() bool { return false }
func (Window) App() string    { return "" }

// Pin is not available on this platform.
func Pin(string) error { return ErrUnsupported }
