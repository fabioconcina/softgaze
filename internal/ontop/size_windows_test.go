//go:build windows

package ontop

import (
	"testing"
	"unsafe"
)

// SendInput rejects an INPUT whose size is not the one C uses.
func TestMouseInputSize(t *testing.T) {
	want := uintptr(28)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		want = 40
	}
	if got := unsafe.Sizeof(mouseInput{}); got != want {
		t.Fatalf("sizeof(INPUT) = %d, want %d", got, want)
	}
}
