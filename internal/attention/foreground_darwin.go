//go:build darwin

package attention

import (
	"os/exec"
	"strings"
)

// Foreground returns the application in front, from lsappinfo (no
// permission needed). macOS does not give window titles without the
// Accessibility permission, so the title stays empty: switching to another
// app is seen, switching between VS Code windows is not.
func Foreground() (Window, error) {
	if _, err := exec.LookPath("lsappinfo"); err != nil {
		return Window{}, ErrUnsupported
	}
	out, err := exec.Command("lsappinfo", "front").Output()
	if err != nil {
		return Window{}, err
	}
	asn := strings.TrimSpace(string(out))
	if asn == "" || strings.HasPrefix(asn, "[ NULL ]") {
		return Window{}, nil
	}
	out, err = exec.Command("lsappinfo", "info", "-only", "name", asn).Output()
	if err != nil {
		return Window{}, err
	}
	return Window{App: parseLSAppInfoName(string(out))}, nil
}
