package server

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"time"
)

// reportIDRe is the shape of the ids the extension and the Softgaze page give
// themselves.
var reportIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const maxReportFolders = 32

// HandleAttentionEditor takes the softgaze extension's word on its VS
// Code window, which macOS does not give (no window titles without the
// Accessibility permission): whether the window has the keyboard, its
// workspace folders, and whether its Softgaze panel is the active editor.
// Body: {"window": "<id>", "focused": bool, "folders": ["/abs"],
// "panel_active": bool, "closed": bool}. 204; ignored with wellbeing off.
func (h *Handlers) HandleAttentionEditor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Window      string   `json:"window"`
		Focused     bool     `json:"focused"`
		Folders     []string `json:"folders"`
		PanelActive bool     `json:"panel_active"`
		Closed      bool     `json:"closed"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil || !reportIDRe.MatchString(body.Window) || len(body.Folders) > maxReportFolders {
		writeJSONError(w, "invalid report", http.StatusBadRequest)
		return
	}
	for _, f := range body.Folders {
		if !filepath.IsAbs(f) || len(f) > 4096 {
			writeJSONError(w, "invalid folder", http.StatusBadRequest)
			return
		}
	}
	if h.Attention != nil {
		if body.Closed {
			h.Attention.Hints().EditorGone(body.Window)
		} else {
			h.Attention.Hints().Editor(body.Window, body.Focused, body.Folders, body.PanelActive, time.Now())
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleAttentionPage takes a Softgaze page's word that it has the keyboard
// (said again every 20 seconds while it does) or lost it. Body:
// {"page": "<id>", "focused": bool}. 204; ignored with wellbeing off.
func (h *Handlers) HandleAttentionPage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Page    string `json:"page"`
		Focused bool   `json:"focused"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil || !reportIDRe.MatchString(body.Page) {
		writeJSONError(w, "invalid report", http.StatusBadRequest)
		return
	}
	if h.Attention != nil {
		h.Attention.Hints().Page(body.Page, body.Focused, time.Now())
	}
	w.WriteHeader(http.StatusNoContent)
}
