package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PIDInfo represents a running Claude Code session from ~/.claude/sessions/{PID}.json.
type PIDInfo struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	StartedAt  int64  `json:"startedAt"`
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
	// Status is what Claude Code is doing: busy, shell, idle or waiting (on
	// the user, see WaitingFor). Empty on versions that do not write it; the
	// window then says it cannot tell (FollowUnsure), as for a value it does
	// not know.
	Status     string `json:"status,omitempty"`
	WaitingFor string `json:"waitingFor,omitempty"`
	// StatusUpdatedAt is when Status last changed (Unix ms); it stays put
	// while the status does.
	StatusUpdatedAt int64 `json:"statusUpdatedAt,omitempty"`
}

// readPIDFiles returns the PID files whose process is still running.
func readPIDFiles(claudeDir string) []PIDInfo {
	sessionsDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}

	var results []PIDInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessionsDir, e.Name()))
		if err != nil {
			continue
		}

		var info PIDInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}

		// Verify PID matches filename (filename is {PID}.json)
		namePID, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil || namePID != info.PID {
			continue
		}

		if !isProcessRunning(info.PID) {
			continue
		}
		results = append(results, info)
	}
	return results
}

// SessionFromPIDFile returns the session a PID file belongs to and its cwd,
// or empty strings when it cannot be read (e.g. it was just removed because
// the process exited).
func SessionFromPIDFile(path string) (sessionID, cwd string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var info PIDInfo
	if json.Unmarshal(data, &info) != nil {
		return "", ""
	}
	return info.SessionID, info.CWD
}
