// Package attention tells whether the user is with the session Softgaze
// follows: whether the window in front belongs to its project. Claude Code
// prompts alone cannot see a switch to another window, a chat or a todo list.
//
// Privacy: the foreground window is read every 2 seconds, only while a
// Softgaze window is open, and reduced on the spot to one verdict per
// followed project (with it, or away). Titles are never stored, logged or
// sent anywhere; only the verdict changes are kept, in memory, for a day.
//
// macOS gives the app in front but no window titles, so the softgaze
// extension and the Softgaze page say which VS Code window and whether a
// Softgaze window has the keyboard (see Hints).
package attention

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ErrUnsupported is returned by Foreground where the platform cannot tell
// which window is in front (e.g. Wayland, or no X display on Linux).
var ErrUnsupported = errors.New("foreground window not available on this platform")

// Window is the window in front.
type Window struct {
	App   string // executable or application name ("Code.exe", "Code")
	Title string // window title; empty where the platform does not give it (macOS)
}

// Verdict is what a foreground window says about the user and a project.
type Verdict int8

const (
	Unknown Verdict = iota // cannot tell (a terminal, a VS Code window on macOS without the extension's word): counts as with it
	With                   // a window of the project, or the Softgaze window
	Away                   // anything else
)

const (
	sampleEvery = 2 * time.Second
	wantFor     = 2 * time.Minute // keep sampling this long after the last request
	keepFor     = 24 * time.Hour
)

// Terminals run Claude Code without saying which project in their title.
var terminals = map[string]bool{
	"windowsterminal": true, "cmd": true, "powershell": true, "pwsh": true, "conhost": true, "wezterm-gui": true,
	"alacritty": true, "kitty": true, "terminal": true, "iterm2": true, "ghostty": true, "warp": true,
	"gnome-terminal-server": true, "konsole": true, "xterm": true, "tilix": true, "foot": true, "hyper": true,
}

// Editors whose window title names the workspace folder.
var editors = map[string]bool{
	"code": true, "code - insiders": true, "visual studio code": true, "cursor": true, "windsurf": true, "vscodium": true, "codium": true,
}

// IsEditor reports whether app is a VS Code-like editor.
func IsEditor(app string) bool { return editors[appKey(app)] }

func appKey(app string) string {
	a := strings.ToLower(strings.TrimSpace(app))
	return strings.TrimSuffix(a, ".exe")
}

// Classify says whether w belongs to the session working in cwd: a window
// whose title names its folder (an editor's may name the parent folder, when
// the workspace was opened one level up).
func Classify(w Window, cwd string) Verdict {
	app := appKey(w.App)
	switch {
	case cwd == "" || (app == "" && w.Title == ""):
		return Unknown
	case app == "lockapp":
		return Away // the screen is locked
	case terminals[app]:
		return Unknown
	}
	if w.Title == "" {
		if editors[app] {
			return Unknown // macOS: an editor is in front, but which window?
		}
		return Away
	}
	project := filepath.Base(cwd)
	if hasSegment(w.Title, project) {
		return With
	}
	if parent := filepath.Base(filepath.Dir(cwd)); editors[app] && parent != project && hasSegment(w.Title, parent) {
		return With
	}
	return Away
}

// hasSegment reports whether title names project as one of its parts: VS
// Code shows "file - folder - Visual Studio Code", the Softgaze window
// "Working · folder", browsers "page - Profile - Browser".
func hasSegment(title, project string) bool {
	project = strings.TrimSpace(project)
	if project == "" {
		return false
	}
	parts := strings.FieldsFunc(title, func(r rune) bool { return r == '|' })
	var segs []string
	for _, p := range parts {
		for _, sep := range []string{" - ", " — ", " · ", " – "} {
			p = strings.ReplaceAll(p, sep, "\x00")
		}
		segs = append(segs, strings.Split(p, "\x00")...)
	}
	want := loose(project)
	for _, s := range segs {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "(Workspace)"))
		if want != "" && loose(s) == want {
			return true
		}
	}
	return false
}

// loose keeps only letters and digits, lowercased, so the folder
// "mission-control" matches a page titled "Mission Control" (the app being
// built, open in a browser).
func loose(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type change struct {
	t time.Time
	v Verdict
}

type projectLog struct {
	since   time.Time // sampling for this project started here
	wanted  time.Time
	changes []change // verdict changes, oldest first
}

// Sampler watches the foreground window for the sessions being followed,
// keyed by their working directory.
type Sampler struct {
	foreground func() (Window, error)
	onChange   func(cwd string, v Verdict)
	hints      *Hints

	mu          sync.Mutex
	projects    map[string]*projectLog // by working directory
	running     bool
	unsupported bool
}

// NewSampler returns a sampler that reports verdict changes to onChange
// (which must not block).
func NewSampler(onChange func(cwd string, v Verdict)) *Sampler {
	return &Sampler{foreground: Foreground, onChange: onChange, hints: newHints(), projects: map[string]*projectLog{}}
}

// Hints are where the extension and the Softgaze page report what is in front
// (see Hints), used where the platform gives no window titles.
func (s *Sampler) Hints() *Hints { return s.hints }

// Supported reports whether this platform can tell the window in front.
func (s *Sampler) Supported() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.unsupported
}

// Want asks for the session working in cwd to be watched for the next two
// minutes, starting the sampling loop if needed.
func (s *Sampler) Want(cwd string, now time.Time) {
	if cwd == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unsupported {
		return
	}
	p := s.projects[cwd]
	if p == nil {
		p = &projectLog{since: now}
		s.projects[cwd] = p
	}
	p.wanted = now
	if !s.running {
		s.running = true
		go s.loop()
	}
}

func (s *Sampler) loop() {
	t := time.NewTicker(sampleEvery)
	defer t.Stop()
	for {
		if !s.sample(time.Now()) {
			return
		}
		<-t.C
	}
}

// sample reads the foreground window once and records verdict changes. It
// returns false when the loop should stop.
func (s *Sampler) sample(now time.Time) bool {
	w, err := s.foreground()
	var h Hint
	if err == nil && w.Title == "" && s.hints != nil {
		h = s.hints.hint(w, now) // macOS: no title, so ask the reports
	}
	s.mu.Lock()
	if errors.Is(err, ErrUnsupported) {
		s.unsupported, s.running = true, false
		s.mu.Unlock()
		return false
	}
	var changed []change
	var names []string
	wanted := false
	for name, p := range s.projects {
		if now.Sub(p.wanted) <= wantFor {
			wanted = true
		}
		// Drop what is older than a day, and projects nobody follows.
		for len(p.changes) > 1 && now.Sub(p.changes[1].t) > keepFor {
			p.changes = p.changes[1:]
		}
		if now.Sub(p.wanted) > keepFor {
			delete(s.projects, name)
			continue
		}
		if err != nil {
			continue // a transient failure: keep the last verdict
		}
		v := classify(w, h, name)
		if n := len(p.changes); n == 0 || p.changes[n-1].v != v {
			p.changes = append(p.changes, change{now, v})
			changed = append(changed, change{now, v})
			names = append(names, name)
		}
	}
	if !wanted {
		s.running = false
	}
	s.mu.Unlock()
	for i := range changed {
		if s.onChange != nil {
			s.onChange(names[i], changed[i].v)
		}
	}
	return wanted
}

// Span summarizes the user's attention to a session over [from, now].
type Span struct {
	// Watched is false when nothing was sampled for the project (an
	// unsupported platform, or sampling has not started): treat as with it.
	Watched bool
	// Resumed is when the user came back after the last absence longer than
	// maxAway; zero when there was none. A stretch restarts there.
	Resumed time.Time
	// Away is the time spent away in shorter absences since the later of
	// from and Resumed, not counting the current one.
	Away time.Duration
	// AwaySince is when the current absence started; zero when the user is
	// with the project now.
	AwaySince time.Time
}

// Attention returns the user's attention to the session in cwd over
// [from, now].
// Unknown verdicts count as with it; time before sampling started is
// assumed with it.
func (s *Sampler) Attention(cwd string, from, now time.Time, maxAway time.Duration) Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.projects[cwd]
	if p == nil || len(p.changes) == 0 {
		return Span{}
	}
	return spanOf(p.changes, from, now, maxAway)
}

func spanOf(changes []change, from, now time.Time, maxAway time.Duration) Span {
	sp := Span{Watched: true}
	start := from
	var awayFrom time.Time // start of the absence in progress during the walk
	for i, c := range changes {
		if !c.t.Before(now) {
			break
		}
		// The verdict holds until the next change, or until now.
		end, current := now, true
		if i+1 < len(changes) && changes[i+1].t.Before(now) {
			end, current = changes[i+1].t, false
		}
		if !end.After(start) || c.v != Away {
			continue
		}
		a := c.t
		if a.Before(start) {
			a = start
		}
		if current {
			awayFrom = a // still away
			break
		}
		if end.Sub(a) > maxAway {
			// A long absence ends the stretch: start over when the user is back.
			sp.Resumed, sp.Away, start = end, 0, end
			continue
		}
		sp.Away += end.Sub(a)
	}
	if !awayFrom.IsZero() {
		sp.AwaySince = awayFrom
	}
	return sp
}
