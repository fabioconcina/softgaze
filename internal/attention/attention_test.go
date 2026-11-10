package attention

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator)+"repo", "id3", "mission-control")
	tests := []struct {
		name string
		w    Window
		want Verdict
	}{
		{"its VS Code window", Window{"Code.exe", "Authn-fe background styl… - mission-control - Visual Studio Code"}, With},
		{"unsaved file", Window{"Code.exe", "● main.go - mission-control - Visual Studio Code"}, With},
		{"multi-root workspace", Window{"Code.exe", "main.go - mission-control (Workspace) - Visual Studio Code"}, With},
		{"workspace opened one level up", Window{"Code.exe", "main.go - id3 - Visual Studio Code"}, With},
		{"another VS Code window", Window{"Code.exe", "Handoff doc - claumon - Visual Studio Code"}, Away},
		{"the Softgaze window", Window{"msedge.exe", "Working · mission-control"}, With},
		{"the app being built, in a browser", Window{"firefox.exe", "Mission Control — Mozilla Firefox"}, With},
		{"a browser on the parent's name is not the editor", Window{"msedge.exe", "id3 - Work - Microsoft Edge"}, Away},
		{"chat", Window{"ms-teams.exe", "Chat | Microsoft Teams"}, Away},
		{"terminal: cannot tell", Window{"WindowsTerminal.exe", "✳ Fix the login"}, Unknown},
		{"locked screen", Window{"LockApp.exe", ""}, Away},
		{"nothing in front", Window{}, Unknown},
		{"macOS editor: cannot tell which window", Window{"Code", ""}, Unknown},
		{"macOS other app", Window{"Slack", ""}, Away},
	}
	for _, tt := range tests {
		if got := Classify(tt.w, cwd); got != tt.want {
			t.Errorf("%s: Classify(%+v) = %v, want %v", tt.name, tt.w, got, tt.want)
		}
	}
	if got := Classify(Window{"Code.exe", "x - claumon - Visual Studio Code"}, ""); got != Unknown {
		t.Errorf("no cwd: %v, want Unknown", got)
	}
}

func TestParsers(t *testing.T) {
	if got := parseLSAppInfoName(`"LSDisplayName"="Visual Studio Code"` + "\n"); got != "Visual Studio Code" {
		t.Errorf("lsappinfo name = %q", got)
	}
	// macOS 27: -only is ignored and the whole record comes back.
	record := "\"Code\" ASN:0x0-0x426426: (in front) \n    bundleID=[ NULL ] \n    bundle path=[ NULL ] \n    executable path=[ NULL ] \n" +
		" !cgsConnection !signalled type=[ NULL ]  flavor=[ NULL ]  Version=[ NULL ]  Arch=!!none \n"
	if got := parseLSAppInfoName(record); got != "Code" {
		t.Errorf("lsappinfo record (macOS 27) name = %q, want Code", got)
	}
	if got := parseLSAppInfoName("\"Google Chrome\" ASN:0x0-0x1d01d: (in front) \n    bundleID=\"com.google.Chrome\"\n"); got != "Google Chrome" {
		t.Errorf("lsappinfo record with a bundle id = %q", got)
	}
	if got := parseLSAppInfoName(""); got != "" {
		t.Errorf("no output = %q", got)
	}
	if got := parseActiveWindow("_NET_ACTIVE_WINDOW(WINDOW): window id # 0x3a00007\n"); got != "0x3a00007" {
		t.Errorf("active window = %q", got)
	}
	if got := parseActiveWindow("_NET_ACTIVE_WINDOW(WINDOW): window id # 0x0\n"); got != "" {
		t.Errorf("no active window = %q", got)
	}
	w := parseXprop("WM_CLASS(STRING) = \"code\", \"Code\"\n_NET_WM_NAME(UTF8_STRING) = \"main.go - \\\"claumon\\\" - Visual Studio Code\"\n")
	if w.App != "Code" || w.Title != `main.go - "claumon" - Visual Studio Code` {
		t.Errorf("xprop = %+v", w)
	}
}

var t0 = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

func m(min float64) time.Time { return t0.Add(time.Duration(min * float64(time.Minute))) }

func TestSpanOf(t *testing.T) {
	const max = 2 * time.Minute
	changes := []change{{m(0), With}, {m(5), Away}, {m(6), With}, {m(10), Away}, {m(15), Unknown}, {m(20), Away}}
	// From 0: a 1-minute absence, then a 5-minute one that ends the stretch
	// (back at 15), then away again since 20.
	sp := spanOf(changes, m(0), m(21), max)
	if !sp.Watched || !sp.Resumed.Equal(m(15)) || sp.Away != 0 || !sp.AwaySince.Equal(m(20)) {
		t.Errorf("span = %+v, want resumed at 15, away since 20", sp)
	}
	// From 0 to 9: just the short absence, with it now.
	sp = spanOf(changes, m(0), m(9), max)
	if !sp.Resumed.IsZero() || sp.Away != time.Minute || !sp.AwaySince.IsZero() {
		t.Errorf("span to 9 = %+v, want 1 minute away", sp)
	}
	// An absence that began before the stretch counts only from its start.
	sp = spanOf([]change{{m(0), Away}, {m(3), With}}, m(2), m(4), max)
	if sp.Away != time.Minute {
		t.Errorf("span from 2 = %+v, want 1 minute away", sp)
	}
}

func TestSampler(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator)+"repo", "claumon")
	var notified []Verdict
	s := NewSampler(func(_ string, v Verdict) { notified = append(notified, v) })
	front := Window{"Code.exe", "a - claumon - Visual Studio Code"}
	s.foreground = func() (Window, error) { return front, nil }
	s.projects[cwd] = &projectLog{since: m(0), wanted: m(0)}

	s.sample(m(0))
	s.sample(m(0.1)) // same window: no change
	front = Window{"ms-teams.exe", "Chat | Microsoft Teams"}
	s.sample(m(1))
	front = Window{"Code.exe", "b - claumon - Visual Studio Code"}
	s.sample(m(1.5))
	if len(notified) != 3 || notified[0] != With || notified[1] != Away || notified[2] != With {
		t.Errorf("changes = %v, want with, away, with", notified)
	}
	if sp := s.Attention(cwd, m(0), m(2), 2*time.Minute); sp.Away != 30*time.Second || !sp.AwaySince.IsZero() {
		t.Errorf("attention = %+v, want 30 s away", sp)
	}
	// Nobody asked for two minutes: the loop stops.
	if s.sample(m(3)) {
		t.Error("sampling should stop without requests")
	}

	s.foreground = func() (Window, error) { return Window{}, ErrUnsupported }
	s.sample(m(4))
	if s.Supported() {
		t.Error("an unsupported platform should be remembered")
	}
}
