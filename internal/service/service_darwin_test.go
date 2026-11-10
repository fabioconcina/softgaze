package service

import (
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestLaunchdPath(t *testing.T) {
	found := func(p string) func(string) (string, error) {
		return func(string) (string, error) { return p, nil }
	}
	missing := func(string) (string, error) { return "", errors.New("not found") }
	tests := []struct {
		name     string
		lookPath func(string) (string, error)
		want     string
	}{
		{"code in /usr/local/bin", found("/usr/local/bin/code"), "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"},
		{"code elsewhere goes first", found("/Users/me/bin/code"), "/Users/me/bin:/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"},
		{"no code", missing, "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"},
	}
	for _, tt := range tests {
		if got := launchdPath(tt.lookPath); got != tt.want {
			t.Errorf("%s: launchdPath = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPlistContentSetsPath(t *testing.T) {
	got := plistContent("/Users/me/.local/bin/softgaze", "/Users/me/A&B:/usr/bin", "/Users/me/.softgaze/softgaze.log")
	want := "<key>EnvironmentVariables</key>\n    <dict>\n        <key>PATH</key>\n        <string>/Users/me/A&amp;B:/usr/bin</string>"
	if !strings.Contains(got, want) {
		t.Errorf("plist has no escaped PATH:\n%s", got)
	}
	if extractExecPath(got) != "/Users/me/.local/bin/softgaze" {
		t.Errorf("exec path = %q", extractExecPath(got))
	}
}

func TestExtractExecPath(t *testing.T) {
	tests := []struct {
		name  string
		plist string
		want  string
	}{
		{
			name: "standard plist",
			plist: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.softgaze</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/test/.local/bin/softgaze</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>`,
			want: "/Users/test/.local/bin/softgaze",
		},
		{
			name:  "no ProgramArguments",
			plist: `<dict><key>Label</key><string>test</string></dict>`,
			want:  "",
		},
		{
			name:  "empty string",
			plist: "",
			want:  "",
		},
		{
			name: "path with spaces",
			plist: `<key>ProgramArguments</key>
    <array>
        <string>/Users/test/my apps/softgaze</string>
    </array>`,
			want: "/Users/test/my apps/softgaze",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExecPath(tt.plist)
			if got != tt.want {
				t.Errorf("extractExecPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A folder name with XML's special characters still gives a plist launchd
// can read, and the path read back is the one installed.
func TestPlistContentEscapesPaths(t *testing.T) {
	exe, logPath := "/Users/me/R&D <tools>/softgaze", "/Users/me/R&D <tools>/softgaze.log"
	got := plistContent(exe, "/usr/bin", logPath)
	d := xml.NewDecoder(strings.NewReader(got))
	d.Strict = true
	var strs []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("plist is not valid XML: %v\n%s", err, got)
		}
		if cd, ok := tok.(xml.CharData); ok && strings.TrimSpace(string(cd)) != "" {
			strs = append(strs, string(cd))
		}
	}
	if !slices.Contains(strs, exe) || !slices.Contains(strs, logPath) {
		t.Errorf("paths not in the plist as written: %q", strs)
	}
	if p := extractExecPath(got); p != exe {
		t.Errorf("extractExecPath = %q, want %q", p, exe)
	}
}

func TestLaunchctlPID(t *testing.T) {
	running := "{\n\t\"Label\" = \"com.softgaze\";\n\t\"LastExitStatus\" = 0;\n\t\"PID\" = 99100;\n\t\"Program\" = \"/Users/me/.local/bin/softgaze\";\n};\n"
	stopped := "{\n\t\"Label\" = \"com.softgaze\";\n\t\"LastExitStatus\" = 256;\n};\n"
	for out, want := range map[string]string{running: "99100", stopped: "", "": ""} {
		if got := launchctlPID(out); got != want {
			t.Errorf("launchctlPID(%q) = %q, want %q", out, got, want)
		}
	}
}
