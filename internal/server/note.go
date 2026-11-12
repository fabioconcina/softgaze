package server

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/fabioconcina/softgaze/internal/parser"
)

// A note is a thought the user sets down in the window while Claude works,
// so they can let it go: something to check when Claude is back, or
// anything else on their mind. It stays with its session, turn after turn,
// until the user marks it done (or clears it), so a thought never goes
// without them seeing it go. Notes stay in memory only: softgaze writes no
// content to disk, and a restart forgets them.
type note struct {
	text string
	seq  uint64 // the order notes were written in
}

const (
	noteMax   = 280 // characters kept of a note
	notesKept = 100 // sessions with a note, at most
)

var noteSpaceRe = regexp.MustCompile(`\s+`)

// cleanNote is the note as kept: one line, trimmed, at most noteMax
// characters.
func cleanNote(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(noteSpaceRe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > noteMax {
		s = strings.TrimSpace(string(r[:noteMax]))
	}
	return s
}

// noteFor is the note set down in f's session ("" when none).
func (h *Handlers) noteFor(f *parser.Follow) string {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	return h.notes[f.SessionID].text
}

// HandleFollowNote keeps the note of a session. Body: {"session": "<id>",
// "text": "..."}; an empty text drops the note (Done). Every open window is
// told to refresh.
func (h *Handlers) HandleFollowNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || !sessionIDRe.MatchString(body.Session) {
		writeJSONError(w, "invalid note", http.StatusBadRequest)
		return
	}
	text := cleanNote(body.Text)
	h.notesMu.Lock()
	if text == "" {
		delete(h.notes, body.Session)
	} else {
		if h.notes == nil {
			h.notes = map[string]note{}
		}
		if _, ok := h.notes[body.Session]; !ok && len(h.notes) >= notesKept {
			// Full: the note written longest ago goes.
			oldest := ""
			for id, n := range h.notes {
				if oldest == "" || n.seq < h.notes[oldest].seq {
					oldest = id
				}
			}
			delete(h.notes, oldest)
		}
		h.noteSeq++
		h.notes[body.Session] = note{text: text, seq: h.noteSeq}
	}
	h.notesMu.Unlock()
	if h.events != nil {
		h.events.SendJSON("session_activity", map[string]string{"reason": "note"})
	}
	writeJSON(w, map[string]string{"note": text})
}
