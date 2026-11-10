package attention

import (
	"strconv"
	"strings"
)

// Parsers for the command-line tools the macOS and Linux readers use, kept
// free of build tags so they are tested everywhere.

// parseLSAppInfoName reads `lsappinfo info -only name` output. Up to macOS
// 26 that is one line, "LSDisplayName"="Code". macOS 27 ignores -only and
// prints the whole record, whose first line starts with the quoted name:
//
//	"Code" ASN:0x0-0x426426: (in front)
//	    bundleID=[ NULL ]
func parseLSAppInfoName(out string) string {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if k = strings.Trim(strings.TrimSpace(k), `"`); ok && (strings.EqualFold(k, "LSDisplayName") || strings.EqualFold(k, "name")) {
			v = strings.TrimSpace(v)
			if u, err := strconv.Unquote(v); err == nil {
				return u
			}
			return strings.Trim(v, `"`)
		}
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if q, err := strconv.QuotedPrefix(strings.TrimSpace(first)); err == nil {
		if u, err := strconv.Unquote(q); err == nil {
			return u
		}
	}
	return ""
}

// parseActiveWindow reads `xprop -root _NET_ACTIVE_WINDOW` output:
// "_NET_ACTIVE_WINDOW(WINDOW): window id # 0x3a00007". Empty when no window
// is active.
func parseActiveWindow(out string) string {
	i := strings.LastIndex(out, "#")
	if i < 0 {
		return ""
	}
	id := strings.TrimSpace(out[i+1:])
	if f := strings.Fields(id); len(f) > 0 {
		id = strings.TrimSuffix(f[0], ",")
	}
	if !strings.HasPrefix(id, "0x") || id == "0x0" {
		return ""
	}
	return id
}

// parseXprop reads `xprop -id <id> WM_CLASS _NET_WM_NAME` output:
//
//	WM_CLASS(STRING) = "code", "Code"
//	_NET_WM_NAME(UTF8_STRING) = "main.go - claumon - Visual Studio Code"
func parseXprop(out string) Window {
	var w Window
	for _, line := range strings.Split(out, "\n") {
		name, val, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(name, "WM_CLASS"):
			// The instance name, then the class: the class names the app.
			parts := strings.Split(val, `", "`)
			w.App = strings.Trim(strings.TrimSpace(parts[len(parts)-1]), `"`)
		case strings.HasPrefix(name, "_NET_WM_NAME"):
			if u, err := strconv.Unquote(strings.TrimSpace(val)); err == nil {
				w.Title = u
			} else {
				w.Title = strings.Trim(strings.TrimSpace(val), `"`)
			}
		}
	}
	return w
}
