// Package ontop moves between softgaze and VS Code: it brings the
// session's editor window to the front and the Softgaze window back.
package ontop

import "errors"

var (
	// ErrUnsupported: softgaze does not move windows itself on this
	// platform (the caller asks the editor's CLI instead).
	ErrUnsupported = errors.New("moving windows is not available on this platform")
	// ErrNotFound: no window matches.
	ErrNotFound = errors.New("no matching window")
	// ErrRefused: Windows kept the window from coming to the front (it
	// only flashes in the taskbar).
	ErrRefused = errors.New("softgaze could not bring the window to the front: Windows only flashed it in the taskbar")
)

// Window is another program's top-level window (on Windows its handle;
// zero, no window, elsewhere).
type Window uintptr
