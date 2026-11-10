package attention

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyWithHints(t *testing.T) {
	sep := string(filepath.Separator)
	cwd := filepath.Join(sep+"Users", "me", "Projects", "softgaze")
	code := Window{"Code", ""}
	tests := []struct {
		name string
		w    Window
		h    Hint
		want Verdict
	}{
		{"no word from the extension", code, Hint{}, Unknown},
		{"its VS Code window", code, Hint{Known: true, Folders: []string{cwd}}, With},
		{"its folder, other case", code, Hint{Known: true, Folders: []string{filepath.Join(sep+"Users", "me", "projects", "SoftGaze")}}, With},
		{"workspace one level up", code, Hint{Known: true, Folders: []string{filepath.Join(sep+"Users", "me", "Projects")}}, With},
		{"a subfolder opened on its own", code, Hint{Known: true, Folders: []string{filepath.Join(cwd, "extension")}}, With},
		{"another VS Code window", code, Hint{Known: true, Folders: []string{filepath.Join(sep+"Users", "me", "Projects", "claumon")}}, Away},
		{"a sibling with a common prefix", code, Hint{Known: true, Folders: []string{cwd + "-old"}}, Away},
		{"an empty VS Code window", code, Hint{Known: true}, Away},
		{"the Softgaze panel, in a window of another project", code, Hint{Known: true, Folders: []string{sep + "elsewhere"}, InSoftgaze: true}, With},
		{"a Softgaze page in a browser", Window{"Google Chrome", ""}, Hint{InSoftgaze: true}, With},
		{"a browser without a Softgaze page", Window{"Google Chrome", ""}, Hint{}, Away},
		{"locked screen beats a stale page", Window{"LockApp.exe", ""}, Hint{InSoftgaze: true}, Away},
		{"a title wins over folders", Window{"Code.exe", "a - claumon - Visual Studio Code"}, Hint{Known: true, Folders: []string{cwd}}, Away},
	}
	for _, tt := range tests {
		if got := classify(tt.w, tt.h, cwd); got != tt.want {
			t.Errorf("%s: classify(%+v, %+v) = %v, want %v", tt.name, tt.w, tt.h, got, tt.want)
		}
	}
}

func TestHints(t *testing.T) {
	h := newHints()
	code := Window{"Code", ""}
	if got := h.hint(code, m(0)); got.Known || got.InSoftgaze {
		t.Errorf("no reports: %+v", got)
	}
	h.Editor("a", true, []string{"/repo/a"}, false, m(0))
	h.Editor("b", false, []string{"/repo/b"}, false, m(0))
	if got := h.hint(code, m(0)); !got.Known || len(got.Folders) != 1 || got.Folders[0] != "/repo/a" {
		t.Errorf("window a has the keyboard: %+v", got)
	}
	// b takes the keyboard; a's word that it lost it never arrives.
	h.Editor("b", true, []string{"/repo/b"}, true, m(1))
	if got := h.hint(code, m(1)); !got.Known || got.Folders[0] != "/repo/b" || !got.InSoftgaze {
		t.Errorf("window b with its Softgaze panel: %+v", got)
	}
	// The editor's word counts only while an editor is in front.
	if got := h.hint(Window{"Slack", ""}, m(1)); got.Known || got.InSoftgaze {
		t.Errorf("Slack in front: %+v", got)
	}
	h.EditorGone("b")
	if got := h.hint(code, m(1)); got.Known {
		t.Errorf("after b closed, no window has the keyboard: %+v", got)
	}

	// A Softgaze page with the keyboard, then silent past its refresh.
	h.Page("p1", true, m(2))
	if got := h.hint(Window{"Google Chrome", ""}, m(2.5)); !got.InSoftgaze {
		t.Errorf("page focused: %+v", got)
	}
	if got := h.hint(Window{"Google Chrome", ""}, m(2).Add(pageFresh+time.Second)); got.InSoftgaze {
		t.Errorf("page silent too long: %+v", got)
	}
	h.Page("p2", true, m(3))
	h.Page("p2", false, m(3.1))
	if got := h.hint(Window{"Google Chrome", ""}, m(3.2)); got.InSoftgaze {
		t.Errorf("page lost the keyboard: %+v", got)
	}

	// Windows that never said goodbye are dropped after a day.
	h.Editor("old", false, nil, false, m(0))
	h.Editor("new", true, nil, false, m(0).Add(editorKeep+time.Minute))
	if _, ok := h.editors["old"]; ok {
		t.Error("a day-old report should be dropped")
	}
}

// On macOS the sampler asks the reports, since there is no title.
func TestSamplerWithHints(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator)+"repo", "softgaze")
	var notified []Verdict
	s := NewSampler(func(_ string, v Verdict) { notified = append(notified, v) })
	s.foreground = func() (Window, error) { return Window{"Code", ""}, nil }
	s.projects[cwd] = &projectLog{since: m(0), wanted: m(0)}

	s.sample(m(0)) // no word yet: cannot tell
	s.Hints().Editor("w1", true, []string{"/repo/other"}, false, m(0.5))
	s.sample(m(0.5))
	s.Hints().Editor("w2", true, []string{cwd}, false, m(1))
	s.sample(m(1))
	if len(notified) != 3 || notified[0] != Unknown || notified[1] != Away || notified[2] != With {
		t.Errorf("changes = %v, want unknown, away, with", notified)
	}
}

// Page reports stay few: stale ones go as new ones arrive, and a burst is
// capped, keeping the latest.
func TestPagesStayBounded(t *testing.T) {
	h := newHints()
	t0 := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 1000; i++ {
		h.Page(fmt.Sprintf("p%d", i), true, t0.Add(time.Duration(i)*time.Second))
	}
	if n := len(h.pages); n > maxPages {
		t.Errorf("%d pages kept, want at most %d", n, maxPages)
	}
	if _, ok := h.pages["p999"]; !ok {
		t.Error("the latest page was dropped")
	}
	h.Page("late", true, t0.Add(time.Hour))
	if n := len(h.pages); n != 1 {
		t.Errorf("%d pages kept after an hour of silence, want only the new one", n)
	}
}
