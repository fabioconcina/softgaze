// Package ontop keeps a window above all others, for Softgaze in
// browsers that cannot float a window themselves (Firefox). The page puts a
// random token in its title; Pin finds the window by it.
package ontop

import "errors"

var (
	// ErrUnsupported: this platform does not let softgaze keep another
	// program's window on top.
	ErrUnsupported = errors.New("keeping a window on top is not available on this platform")
	// ErrNotFound: no window has the token in its title (yet).
	ErrNotFound = errors.New("no window with that title")
	// ErrRefused: Windows kept the window from coming to the front (it
	// only flashes in the taskbar).
	ErrRefused = errors.New("softgaze could not bring the window to the front: Windows only flashed it in the taskbar")
)

// Window is another program's top-level window (on Windows its handle;
// zero, no window, elsewhere).
type Window uintptr
