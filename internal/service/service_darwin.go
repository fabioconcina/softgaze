package service

import (
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fabioconcina/softgaze/internal/config"
)

const (
	darwinLabel = "com.softgaze"
)

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", darwinLabel+".plist"), nil
}

// plistContent is the LaunchAgent that runs execPath with PATH set to
// pathEnv, logging to logPath. Every value is escaped for XML (a folder
// name can hold an &).
func plistContent(execPath, pathEnv, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>%s</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>`, darwinLabel, xmlText(execPath), xmlText(pathEnv), xmlText(logPath), xmlText(logPath))
}

// launchdPath is the PATH the service runs with. launchd starts an agent
// with /usr/bin:/bin:/usr/sbin:/sbin only, which leaves out VS Code's `code`
// command (linked into /usr/local/bin, or Homebrew's bin), so Enter in
// Softgaze could not reach VS Code. The folder `code` is found in at install
// time goes first.
func launchdPath(lookPath func(string) (string, error)) string {
	var dirs []string
	if p, err := lookPath("code"); err == nil {
		dirs = append(dirs, filepath.Dir(p))
	}
	dirs = append(dirs, "/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	seen := map[string]bool{}
	out := dirs[:0]
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return strings.Join(out, ":")
}

// xmlText escapes s for a plist <string>.
func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s)) //nolint:errcheck // a strings.Builder does not fail
	return b.String()
}

func Install(execPath string) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	clearProvenance(execPath)

	content := plistContent(execPath, launchdPath(exec.LookPath), config.LogPath())
	// Installed already: stop it first, so installing again applies the new
	// plist (launchd will not load a job that is loaded).
	exec.Command("launchctl", "unload", path).Run()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("loading plist: %w", err)
	}

	return nil
}

func Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	exec.Command("launchctl", "unload", path).Run()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("launchctl", "list", darwinLabel).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Could not find service") {
			return "not installed", nil
		}
		return "not running", nil
	}
	if pid := launchctlPID(string(out)); pid != "" {
		return fmt.Sprintf("running (PID %s)", pid), nil
	}
	return "installed (not running)", nil
}

// launchctlPID is the PID in `launchctl list <label>` output, a line
// `"PID" = 123;`, or "" when the job is not running.
func launchctlPID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != `"PID"` {
			continue
		}
		if pid := strings.TrimSuffix(strings.TrimSpace(v), ";"); pid != "0" {
			return pid
		}
	}
	return ""
}

func Restart() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("service not installed: run 'softgaze service install' first")
	}

	// Read the plist to find the binary path and clear provenance before restart
	data, err := os.ReadFile(path)
	if err == nil {
		if execPath := extractExecPath(string(data)); execPath != "" {
			clearProvenance(execPath)
		}
	}

	exec.Command("launchctl", "unload", path).Run()
	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("reloading plist: %w", err)
	}
	return nil
}

// clearProvenance removes macOS quarantine/provenance attributes and re-signs
// the binary so AppleSystemPolicy allows execution.
func clearProvenance(path string) {
	exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
	exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
	exec.Command("codesign", "--force", "--sign", "-", path).Run()
}

// extractExecPath parses the binary path from a launchd plist, undoing the
// XML escapes plistContent wrote.
func extractExecPath(plist string) string {
	// Find the first <string> after <key>ProgramArguments</key><array>
	const marker = "<key>ProgramArguments</key>"
	idx := strings.Index(plist, marker)
	if idx < 0 {
		return ""
	}
	rest := plist[idx+len(marker):]
	const open = "<string>"
	const close = "</string>"
	si := strings.Index(rest, open)
	if si < 0 {
		return ""
	}
	rest = rest[si+len(open):]
	ei := strings.Index(rest, close)
	if ei < 0 {
		return ""
	}
	return html.UnescapeString(strings.TrimSpace(rest[:ei]))
}
