package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/fabioconcina/softgaze/internal/attention"
	"github.com/fabioconcina/softgaze/internal/ontop"
	"github.com/fabioconcina/softgaze/internal/parser"
)

// sessionIDRe accepts Claude Code session ids (UUIDs) and nothing that could
// steer FindSessionFile out of the projects directory.
var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// HandleFollow serves the follow-along window (see parser.Follow): the
// session ?session=<id> (the one the window locked onto), or without it the
// running session the user prompted most recently, which a newly opened
// window locks onto. Response: {"session": {...}}, or {"session": null} when
// nothing runs. Computed on request only: nobody pays for it until a window
// is open.
func (h *Handlers) HandleFollow(w http.ResponseWriter, r *http.Request) {
	// The switch list needs the prompt timeline; its first read takes a few
	// seconds, so start it now rather than on the user's click.
	h.warmWork.Do(func() { go parser.CurrentWorkPeriod(h.claudeDir, time.Now()) })

	id := r.URL.Query().Get("session")
	if id != "" && !sessionIDRe.MatchString(id) {
		writeJSONError(w, "invalid session id", http.StatusBadRequest)
		return
	}
	var f *parser.Follow
	if id == "" {
		if running := parser.FollowSessions(h.claudeDir); len(running) > 0 {
			f = running[0]
		}
	} else {
		var err error
		if f, err = parser.FollowSession(h.claudeDir, id); err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if f != nil && f.Running && h.Wellbeing {
		now := time.Now()
		f.Focus = h.focusFor(f, now)
		// At the user's turn after a long run: a break would fit here.
		if f.State == parser.FollowFinished || f.State == parser.FollowNeedsYou {
			if s := parser.AtItSince(h.claudeDir, now); !s.IsZero() && now.Sub(s) >= breakAfter {
				f.AtItSince = &s
			}
		}
	}
	writeJSON(w, map[string]*parser.Follow{"session": f})
}

const (
	focusAwayMax   = 2 * time.Minute // away longer than this ends a stretch
	focusSaveEvery = 20 * time.Second
	breakAfter     = 90 * time.Minute // a run this long, and Softgaze suggests a break
)

// focusFor is the user's current stretch on f's session: from the prompts
// (parser.FocusFor), then narrowed by what was in front on screen. Time
// away in short absences pauses it; an absence over two minutes ends it,
// and it starts over when the user is back. Each stretch is recorded for
// the minutes of the work period in the switch list.
func (h *Handlers) focusFor(f *parser.Follow, now time.Time) *parser.FocusStretch {
	fo := parser.FocusFor(h.claudeDir, f.SessionID, f.State == parser.FollowWorking, now)
	if fo == nil {
		return nil
	}
	away := time.Duration(0)
	if h.Attention != nil && h.Attention.Supported() {
		h.Attention.Want(f.CWD, now)
		sp := h.Attention.Attention(f.CWD, fo.Since, now, focusAwayMax)
		fo.Watched = sp.Watched
		if !sp.Resumed.IsZero() {
			fo.Since = sp.Resumed
			fo.Prompts = 0
			for _, t := range fo.PromptTimes {
				if !t.Before(sp.Resumed) {
					fo.Prompts++
				}
			}
		}
		if !sp.AwaySince.IsZero() {
			if now.Sub(sp.AwaySince) > focusAwayMax {
				// Gone too long: the stretch ended when the user left.
				h.saveFocus(f.SessionID, fo, sp.AwaySince.Sub(fo.Since)-sp.Away)
				return nil
			}
			t := sp.AwaySince
			fo.AwaySince = &t
			away = now.Sub(t)
		}
		fo.AwayMs = sp.Away.Milliseconds()
		away += sp.Away
	}
	h.saveFocus(f.SessionID, fo, now.Sub(fo.Since)-away)
	return fo
}

// pinTokenRe is the shape of the token a Softgaze page puts in its title
// to be found and kept on top.
var pinTokenRe = regexp.MustCompile(`^sg-pin-[a-z0-9]{8,32}$`)

// HandleFollowPinInfo says whether this machine can keep a browser window on
// top for Softgaze: {"supported": bool}.
func (h *Handlers) HandleFollowPinInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"supported": ontop.Supported()})
}

// HandleFollowPin keeps the Softgaze window above all others, for
// browsers that cannot float one (Firefox). Body: {"token": "sg-pin-..."},
// the token the page has just put in its title.
func (h *Handlers) HandleFollowPin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil || !pinTokenRe.MatchString(body.Token) {
		writeJSONError(w, "invalid token", http.StatusBadRequest)
		return
	}
	if !ontop.Supported() {
		writeJSONError(w, ontop.ErrUnsupported.Error(), http.StatusNotImplemented)
		return
	}
	// The browser updates its window title a moment after the page does.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := ontop.Pin(body.Token)
		switch {
		case err == nil:
			writeJSON(w, map[string]bool{"pinned": true})
			return
		case !errors.Is(err, ontop.ErrNotFound):
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		case time.Now().After(deadline):
			writeJSONError(w, "window not found", http.StatusNotFound)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// HandleFollowFocus brings the user to the session's VS Code window and its
// Claude Code tab. Body: {"session": "<id>"}. On Windows softgaze finds the
// editor window whose title names the project and brings it forward (never
// opening a new one); elsewhere it asks the `code` CLI to focus the window
// that has the session's folder. Then it opens the Claude Code extension's
// link for the session, which reveals that session's tab in the window in
// front (a window can hold several sessions).
func (h *Handlers) HandleFollowFocus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil || !sessionIDRe.MatchString(body.Session) {
		writeJSONError(w, "invalid session id", http.StatusBadRequest)
		return
	}
	f, err := parser.FollowSession(h.claudeDir, body.Session)
	if err != nil || f == nil || f.CWD == "" {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}
	// The request comes from the Softgaze window, in front: remembered for the
	// way back (POST /api/follow/back).
	if from := ontop.Foreground(); from != 0 {
		h.viewWinMu.Lock()
		h.viewWin, h.viewApp = from, from.App()
		h.viewWinMu.Unlock()
	}
	editor := ""
	settle := focusSettle
	err = ontop.Focus(func(app, title string) bool {
		ok := attention.IsEditor(app) && attention.Classify(attention.Window{App: app, Title: title}, f.CWD) == attention.With
		if ok {
			editor = app
		}
		return ok
	})
	if errors.Is(err, ontop.ErrUnsupported) {
		if runtime.GOOS == "darwin" {
			editor, err = focusEditorMac(f.CWD)
			settle = focusSettleMac
		} else {
			err = focusWithCLI(f.CWD)
		}
	}
	switch {
	case err == nil:
		// VS Code hands a link to the window last in front: give the one
		// just brought forward a moment to count as that.
		// Only for a session the extension runs: for one in a terminal the
		// link would open a second copy of it in the editor.
		if f.Entrypoint == "claude-vscode" {
			time.Sleep(settle)
			if err := OpenURL(sessionURL(editor, f.SessionID)); err != nil {
				log.Printf("[focus] opening the session tab: %v", err)
			}
		}
		writeJSON(w, map[string]bool{"focused": true})
	case errors.Is(err, ontop.ErrNotFound):
		writeJSONError(w, "no VS Code window for "+f.Project, http.StatusNotFound)
	default:
		writeJSONError(w, err.Error(), http.StatusNotImplemented)
	}
}

// HandleFollowBack brings the Softgaze window last seen in front back to the
// front, restored if minimized and with the keyboard: the way back from
// VS Code (the extension's Ctrl+Alt+J). Where softgaze cannot move
// windows (macOS) or saw none, a Softgaze panel open in any VS Code window is
// asked to come forward (focus_back; the extension raises it). 404 when
// neither applies (the extension then shows its own).
func (h *Handlers) HandleFollowBack(w http.ResponseWriter, r *http.Request) {
	h.viewWinMu.Lock()
	win, app := h.viewWin, h.viewApp
	h.viewWinMu.Unlock()
	if win != 0 && app != "" && win.App() == app {
		if !win.Activate() {
			writeJSONError(w, ontop.ErrRefused.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]bool{"back": true})
		return
	}
	if h.events != nil && h.events.Embedded() > 0 {
		h.events.SendJSON("focus_back", map[string]string{"reason": "back"})
		writeJSON(w, map[string]bool{"back": true})
		return
	}
	writeJSONError(w, "no Softgaze window", http.StatusNotFound)
}

// focusSettle: the pause between bringing a window forward and handing VS
// Code the session link. On macOS VS Code gathers opened folders for 100 ms
// before it acts on them, so the pause is longer there.
const (
	focusSettle    = 150 * time.Millisecond
	focusSettleMac = 400 * time.Millisecond
)

// macEditors are the VS Code bundles softgaze hands a folder to, with
// the name sessionURL knows them by.
var macEditors = []struct{ bundleID, name string }{
	{"com.microsoft.VSCode", "Code"},
	{"com.microsoft.VSCodeInsiders", "Code - Insiders"},
}

// focusEditorMac hands dir to the running VS Code through Launch Services
// (open -b), which focuses the window that has the folder, or opens one.
// Not the `code` command: on macOS it starts a second VS Code process to
// pass the folder on (its launcher runs open -n), which shows up as another
// VS Code in the Dock. Falls back to `code` when VS Code cannot be reached
// that way.
func focusEditorMac(dir string) (editor string, err error) {
	pick := macEditors[0]
	for _, e := range macEditors {
		if out, err := exec.Command("lsappinfo", "find", "bundleid="+e.bundleID).Output(); err == nil && strings.TrimSpace(string(out)) != "" {
			pick = e // the one running
			break
		}
	}
	cmd := exec.Command("open", "-b", pick.bundleID, dir)
	cmd.Env = editorEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		log.Printf("[focus] open -b %s: %v (trying the code command)", pick.bundleID, err)
		return "", focusWithCLI(dir)
	}
	return pick.name, nil
}

// sessionURL is the Claude Code extension's link that reveals a session's
// tab (or opens it): vscode://anthropic.claude-code/open?session=<id>, with
// the Insiders scheme when that is the editor in front.
func sessionURL(editor, sessionID string) string {
	scheme := "vscode"
	if strings.Contains(strings.ToLower(editor), "insiders") {
		scheme = "vscode-insiders"
	}
	return scheme + "://anthropic.claude-code/open?session=" + url.QueryEscape(sessionID)
}

// OpenURL hands u to the program registered for its scheme (a browser, or
// VS Code for its own links), without VS Code's variables in its env.
func OpenURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	cmd.Env = editorEnv(os.Environ())
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reap it; the handler reports nothing useful
	return nil
}

// focusWithCLI asks VS Code to focus the window that has dir open (it
// reuses the window when one already has it).
func focusWithCLI(dir string) error {
	code, err := exec.LookPath("code")
	if err != nil {
		return errors.New("the VS Code `code` command is not on the PATH")
	}
	cmd := exec.Command(code, dir)
	cmd.Env = editorEnv(os.Environ())
	return cmd.Start()
}

// editorEnv is env without what a VS Code process leaves to its children
// (softgaze started from its terminal, a task or Claude Code inherits it).
// With ELECTRON_RUN_AS_NODE set, the VS Code that opens a link starts as
// plain Node and quietly does nothing.
func editorEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if u := strings.ToUpper(k); u == "ELECTRON_RUN_AS_NODE" || strings.HasPrefix(u, "VSCODE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// saveFocus records a stretch at most every 20 seconds.
func (h *Handlers) saveFocus(session string, fo *parser.FocusStretch, focused time.Duration) {
	if h.store == nil || focused <= 0 {
		return
	}
	key := session + "|" + fo.Since.UTC().Format(time.RFC3339)
	now := time.Now()
	h.focusMu.Lock()
	if h.focusSaved == nil {
		h.focusSaved = map[string]time.Time{}
	}
	if last, ok := h.focusSaved[key]; ok && now.Sub(last) < focusSaveEvery {
		h.focusMu.Unlock()
		return
	}
	h.focusSaved[key] = now
	for k, t := range h.focusSaved {
		if now.Sub(t) > time.Hour {
			delete(h.focusSaved, k)
		}
	}
	h.focusMu.Unlock()
	if err := h.store.SaveFocusStretch(session, fo.Since, focused, fo.Prompts); err != nil {
		log.Printf("[focus] %v", err)
	}
}

type followSessionsResponse struct {
	// Sessions are all running sessions, the one prompted most recently first.
	Sessions []*parser.Follow `json:"sessions"`
	// Current lists the ids of those the user prompted in the current work
	// period (see parser.WorkPeriod): what the switch list shows first.
	Current []string `json:"current"`
	// Since is when the current work period started; null without history.
	Since *time.Time `json:"since"`
	// BreakMinutes is the pause that ends a working day, and Learned whether
	// it was fitted to the user's history.
	BreakMinutes int  `json:"break_minutes"`
	Learned      bool `json:"learned"`
	// FocusedMinutes are the minutes of focus stretches in the work period,
	// across sessions (the measure the window's counter shows). Plain
	// minutes, never a count of turns or a record. Only with wellbeing on.
	FocusedMinutes *int `json:"focused_minutes,omitempty"`
}

// HandleFollowSessions lists the running sessions the window can switch to,
// marking the ones in the current work period. Asked for only when the user
// opens the switch list.
func (h *Handlers) HandleFollowSessions(w http.ResponseWriter, r *http.Request) {
	sessions := parser.FollowSessions(h.claudeDir)
	now := time.Now()
	wp := parser.CurrentWorkPeriod(h.claudeDir, now)
	resp := followSessionsResponse{
		Sessions:     sessions,
		Current:      []string{},
		BreakMinutes: int(wp.Break.Minutes()),
		Learned:      wp.Learned,
	}
	if !wp.Since.IsZero() {
		resp.Since = &wp.Since
	}
	// Without history the period has no start: count from midnight.
	from := wp.Since
	if from.IsZero() {
		y, m, d := now.Date()
		from = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	}
	if h.Wellbeing {
		m := 0
		if h.store != nil {
			if d, err := h.store.FocusSince(from); err == nil {
				m = int(d.Minutes())
			}
		}
		resp.FocusedMinutes = &m
	}
	for _, f := range sessions {
		if f.LastPromptAt != nil && !f.LastPromptAt.Before(wp.Since) {
			resp.Current = append(resp.Current, f.SessionID)
		}
	}
	writeJSON(w, resp)
}
