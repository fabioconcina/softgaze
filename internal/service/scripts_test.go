package service

import (
	"strings"
	"testing"
)

func TestUnitContentQuotesExecStart(t *testing.T) {
	tests := []struct{ path, want string }{
		{"/home/me/.local/bin/softgaze", `ExecStart="/home/me/.local/bin/softgaze"`},
		{"/home/me/my apps/softgaze", `ExecStart="/home/me/my apps/softgaze"`},
		{`/home/me/100%/$HOME/a"b\c`, `ExecStart="/home/me/100%%/$$HOME/a\"b\\c"`},
	}
	for _, tt := range tests {
		got := unitContent(tt.path)
		if !strings.Contains(got, "\n"+tt.want+"\n") {
			t.Errorf("unit for %q has no %s:\n%s", tt.path, tt.want, got)
		}
	}
}

func TestVBSScript(t *testing.T) {
	exe, logPath := `C:\Users\Some One\bin\softgaze.exe`, `C:\Users\Some One\.softgaze\softgaze.log`
	got := vbsScript(exe, logPath)
	want := `CreateObject("WScript.Shell").Run """C:\Users\Some One\bin\softgaze.exe"" -log ""C:\Users\Some One\.softgaze\softgaze.log""", 0, False` + "\r\n"
	if got != want {
		t.Errorf("vbsScript =\n%s\nwant\n%s", got, want)
	}
	if p := extractVBSPath(got); p != exe {
		t.Errorf("extractVBSPath(vbsScript) = %q, want %q", p, exe)
	}
}

func TestExtractVBSPath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "written before paths were quoted",
			content: "CreateObject(\"WScript.Shell\").Run \"C:\\Users\\test\\softgaze.exe\", 0, False\r\n",
			want:    `C:\Users\test\softgaze.exe`,
		},
		{
			name:    "old script, path with spaces",
			content: "CreateObject(\"WScript.Shell\").Run \"C:\\Program Files\\softgaze\\softgaze.exe\", 0, False\r\n",
			want:    `C:\Program Files\softgaze\softgaze.exe`,
		},
		{
			name:    "quoted path",
			content: vbsScript(`C:\Program Files\softgaze\softgaze.exe`, `C:\x.log`),
			want:    `C:\Program Files\softgaze\softgaze.exe`,
		},
		{name: "empty content", content: "", want: ""},
		{name: "no Run keyword", content: "something else entirely", want: ""},
		{name: "unterminated", content: `.Run ""C:\x`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractVBSPath(tt.content); got != tt.want {
				t.Errorf("extractVBSPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
