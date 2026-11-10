package attention

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Hints are what the softgaze VS Code extension and the Softgaze page say
// about the window in front, for platforms that give no window titles
// (macOS, without the Accessibility permission softgaze does not ask
// for). The extension in each VS Code window reports whether that window has
// the keyboard, its workspace folders and whether its Softgaze panel is the
// active editor; a Softgaze page reports when it has the keyboard. Kept in
// memory only, the latest report per window or page, never logged.
type Hints struct {
	mu      sync.Mutex
	editors map[string]*editorWindow // by the extension's window id
	pages   map[string]time.Time     // Softgaze pages with the keyboard: when they last said so
}

type editorWindow struct {
	focused     bool
	folders     []string
	panelActive bool
	at          time.Time
}

// Hint is what the reports say about the window in front.
type Hint struct {
	// Known is true when an editor is in front and the extension said
	// which of its windows has the keyboard; Folders are that window's
	// workspace folders (none: an empty window).
	Known   bool
	Folders []string
	// InSoftgaze is true when a Softgaze window has the keyboard: the
	// extension's panel, or a Softgaze page in a browser.
	InSoftgaze bool
}

const (
	// A Softgaze page with the keyboard says so again every 20 seconds; one
	// silent for longer has gone (closed without a word).
	pageFresh = 50 * time.Second
	// Reports from windows that never said goodbye are dropped after this.
	editorKeep = 24 * time.Hour
	maxEditors = 64
	maxPages   = 16
)

func newHints() *Hints {
	return &Hints{editors: map[string]*editorWindow{}, pages: map[string]time.Time{}}
}

// Editor records the extension's report for its VS Code window. Only one
// window has the keyboard: one that says it has clears the others, in case
// their word that they lost it never arrived.
func (h *Hints) Editor(id string, focused bool, folders []string, panelActive bool, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if focused {
		for _, e := range h.editors {
			e.focused = false
		}
	}
	h.editors[id] = &editorWindow{focused: focused, folders: append([]string{}, folders...), panelActive: panelActive, at: now}
	for k, e := range h.editors {
		if !e.focused && now.Sub(e.at) > editorKeep {
			delete(h.editors, k)
		}
	}
	if len(h.editors) > maxEditors {
		h.dropOldest()
	}
}

func (h *Hints) dropOldest() {
	var oldest string
	for k, e := range h.editors {
		if oldest == "" || e.at.Before(h.editors[oldest].at) {
			oldest = k
		}
	}
	delete(h.editors, oldest)
}

// EditorGone forgets a VS Code window that closed.
func (h *Hints) EditorGone(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.editors, id)
}

// Page records whether a Softgaze page has the keyboard. Pages silent for
// longer than pageFresh are forgotten here too, not only when asked about
// (where titles are given, nobody asks), so reloads do not pile up.
func (h *Hints) Page(id string, focused bool, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, at := range h.pages {
		if now.Sub(at) > pageFresh {
			delete(h.pages, k)
		}
	}
	if !focused {
		delete(h.pages, id)
		return
	}
	h.pages[id] = now
	for len(h.pages) > maxPages {
		var oldest string
		for k, at := range h.pages {
			if oldest == "" || at.Before(h.pages[oldest]) {
				oldest = k
			}
		}
		delete(h.pages, oldest)
	}
}

// hint is what the reports say about w, the window in front. Only asked
// for when the platform gives no title.
func (h *Hints) hint(w Window, now time.Time) Hint {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out Hint
	for id, at := range h.pages {
		if now.Sub(at) > pageFresh {
			delete(h.pages, id)
			continue
		}
		out.InSoftgaze = true
	}
	if IsEditor(w.App) {
		for _, e := range h.editors {
			if e.focused {
				out.Known = true
				out.Folders = e.folders
				out.InSoftgaze = out.InSoftgaze || e.panelActive
				break
			}
		}
	}
	return out
}

// classify is Classify with the reports: a Softgaze window with the keyboard
// is with the session, and an editor window is with it when one of its
// folders holds the session's folder (or sits inside it).
func classify(w Window, h Hint, cwd string) Verdict {
	if cwd == "" || appKey(w.App) == "lockapp" {
		return Classify(w, cwd)
	}
	if h.InSoftgaze {
		return With
	}
	if w.Title == "" && h.Known && IsEditor(w.App) {
		if inFolders(h.Folders, cwd) {
			return With
		}
		return Away
	}
	return Classify(w, cwd)
}

// inFolders reports whether cwd is one of folders, inside one (a
// workspace opened one level up), or holds one (a subfolder opened on its
// own). Case-insensitive, as macOS paths are.
func inFolders(folders []string, cwd string) bool {
	c := strings.ToLower(filepath.Clean(cwd))
	for _, f := range folders {
		f = strings.ToLower(filepath.Clean(f))
		if f == c || within(c, f) || within(f, c) {
			return true
		}
	}
	return false
}

// within reports whether path p lies strictly inside dir.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
