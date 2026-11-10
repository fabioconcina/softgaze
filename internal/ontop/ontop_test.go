package ontop

import (
	"errors"
	"testing"
)

func TestPinUnknownWindow(t *testing.T) {
	if !Supported() {
		t.Skip("keeping windows on top is not available here")
	}
	if err := Pin("sg-pin-no-such-window-zq81x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Pin(unknown) = %v, want ErrNotFound", err)
	}
}
