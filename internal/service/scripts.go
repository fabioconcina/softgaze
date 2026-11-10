package service

import "strings"

// The start-on-login files for Linux and Windows. Plain text, built here
// for every platform so their tests run wherever softgaze is developed.

// unitContent is the systemd user unit that runs softgaze. The service
// logs to the journal: journalctl --user -u softgaze.
func unitContent(execPath string) string {
	return `[Unit]
Description=softgaze: a calm window for one Claude Code session
After=network.target

[Service]
Type=simple
ExecStart=` + systemdQuote(execPath) + `
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`
}

// systemdQuote makes s one word of a unit's command line: in double quotes
// (it may hold spaces), with backslashes and quotes escaped and systemd's
// specifiers (%) and variables ($) doubled so they stay literal.
func systemdQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s)
	return `"` + s + `"`
}

// vbsScript is the Startup folder script that runs softgaze with no
// window (0) and without waiting (False). There is no console to log to,
// so the log goes to logPath. The command line quotes both paths (a user
// folder can have spaces); a VBScript string doubles its quotes.
func vbsScript(execPath, logPath string) string {
	cmd := `"` + execPath + `" -log "` + logPath + `"`
	return `CreateObject("WScript.Shell").Run "` + strings.ReplaceAll(cmd, `"`, `""`) + `", 0, False` + "\r\n"
}

// extractVBSPath pulls the exe path from a script vbsScript wrote, or from
// one written before the path was quoted (.Run "C:\...\softgaze.exe", ...).
func extractVBSPath(content string) string {
	start := strings.Index(content, `.Run "`)
	if start == -1 {
		return ""
	}
	rest := content[start+len(`.Run "`):]
	quote := `"`
	if strings.HasPrefix(rest, `""`) {
		rest, quote = rest[2:], `""`
	}
	end := strings.Index(rest, quote)
	if end == -1 {
		return ""
	}
	return rest[:end]
}
