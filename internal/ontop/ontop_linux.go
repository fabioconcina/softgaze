//go:build linux

package ontop

// Focus is not done here on Linux: the caller asks the editor's CLI.
func Focus(func(app, title string) bool) error { return ErrUnsupported }

// Foreground is zero: softgaze does not track windows here.
func Foreground() Window { return 0 }

// Activate and App do nothing without a window.
func (Window) Activate() bool { return false }
func (Window) App() string    { return "" }
