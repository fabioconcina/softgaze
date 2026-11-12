package server

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/fabioconcina/softgaze/internal/parser"
)

// Setting a session aside takes it out of the switch list and the arrow
// keys: a session the user keeps open but is done with for now. The window
// never picks it by itself either. It comes back on its own once the user
// prompts it again, or when they pick it from the list. Kept in memory
// only, like notes: a restart brings every session back.
type asideMark struct {
	prompted time.Time // the session's last prompt when it was set aside
	seq      uint64    // the order sessions were set aside in
}

const asideKept = 100 // sessions set aside, at most

func lastPrompt(f *parser.Follow) time.Time {
	if f.LastPromptAt != nil {
		return *f.LastPromptAt
	}
	return time.Time{}
}

// isAside reports whether f's session is set aside and has not been
// prompted since; a prompt since ends it.
func (h *Handlers) isAside(f *parser.Follow) bool {
	h.asideMu.Lock()
	defer h.asideMu.Unlock()
	m, ok := h.aside[f.SessionID]
	if !ok {
		return false
	}
	if lastPrompt(f).After(m.prompted) {
		delete(h.aside, f.SessionID)
		return false
	}
	return true
}

// HandleFollowAside sets a session aside, or brings it back. Body:
// {"session": "<id>", "aside": true|false}. Every open window is told to
// refresh.
func (h *Handlers) HandleFollowAside(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
		Aside   bool   `json:"aside"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || !sessionIDRe.MatchString(body.Session) {
		writeJSONError(w, "invalid session", http.StatusBadRequest)
		return
	}
	if !body.Aside {
		h.asideMu.Lock()
		delete(h.aside, body.Session)
		h.asideMu.Unlock()
	} else {
		f, err := parser.FollowSession(h.claudeDir, body.Session)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if f == nil {
			writeJSONError(w, "no such session", http.StatusNotFound)
			return
		}
		h.asideMu.Lock()
		if h.aside == nil {
			h.aside = map[string]asideMark{}
		}
		if _, ok := h.aside[body.Session]; !ok && len(h.aside) >= asideKept {
			// Full: the session set aside longest ago comes back.
			oldest := ""
			for id, m := range h.aside {
				if oldest == "" || m.seq < h.aside[oldest].seq {
					oldest = id
				}
			}
			delete(h.aside, oldest)
		}
		h.asideSeq++
		h.aside[body.Session] = asideMark{prompted: lastPrompt(f), seq: h.asideSeq}
		h.asideMu.Unlock()
	}
	if h.events != nil {
		h.events.SendJSON("session_activity", map[string]string{"reason": "aside"})
	}
	writeJSON(w, map[string]bool{"aside": body.Aside})
}
