//go:build !windows && !darwin && !linux

package attention

// Foreground is not available on this platform.
func Foreground() (Window, error) { return Window{}, ErrUnsupported }
