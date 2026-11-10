package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type sessionFile struct {
	path    string
	modTime time.Time
	size    int64
}

// enumerateSessionFiles walks claudeDir/projects/*/ for *.jsonl and returns
// their paths, sizes and mtimes. Stat failures are skipped silently.
func enumerateSessionFiles(claudeDir string) ([]sessionFile, error) {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("reading projects directory: %w", err)
	}

	var files []sessionFile
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(projectsDir, projEntry.Name(), "*.jsonl"))
		if err != nil {
			continue
		}
		for _, f := range matches {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			files = append(files, sessionFile{path: f, modTime: info.ModTime(), size: info.Size()})
		}
	}
	return files, nil
}

// FindSessionFile finds the JSONL file for a session ID ("" when there is
// none).
func FindSessionFile(claudeDir, sessionID string) string {
	return findSessionFiles(claudeDir, []string{sessionID})[sessionID]
}

// findSessionFiles finds the transcripts of the given sessions with one
// listing of the projects directory: session ID to path, for those found.
func findSessionFiles(claudeDir string, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return out
	}
	for _, id := range ids {
		if _, done := out[id]; done || id == "" {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(projectsDir, e.Name(), id+".jsonl")
			if _, err := os.Stat(path); err == nil {
				out[id] = path
				break
			}
		}
	}
	return out
}

// contentBlock represents a single block in a Claude message content array.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// parseContentBlocks parses message content as either a string or array of blocks.
// Returns nil if content is empty or unparseable.
func parseContentBlocks(content json.RawMessage) []contentBlock {
	if len(content) == 0 {
		return nil
	}

	// Try as string first
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return []contentBlock{{Type: "text", Text: s}}
	}

	// Try as array of content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		return blocks
	}
	return nil
}

// normalizeResultContent handles the two shapes tool_result.content can take:
// a bare string, or an array of {type:"text", text:"..."} blocks.
func normalizeResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
