package parser

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Transcripts are append-only, so each file's builder keeps its read offset
// and later calls parse only the lines added since. A file that shrank (or was
// rewritten) is parsed again from the start.

const (
	followIdleEvict  = 2 * time.Hour    // drop builders nobody asked for in this long
	followMaxLineLen = 32 * 1024 * 1024 // skip pathological lines rather than parse them
)

type followFile struct {
	mu       sync.Mutex
	offset   int64
	modTime  time.Time
	b        *followBuilder
	lastUsed time.Time
}

var followFiles = struct {
	sync.Mutex
	m map[string]*followFile
}{m: map[string]*followFile{}}

func getFollowFile(path string, now time.Time) *followFile {
	followFiles.Lock()
	defer followFiles.Unlock()
	for p, f := range followFiles.m {
		if now.Sub(f.lastUsed) > followIdleEvict && f.mu.TryLock() {
			delete(followFiles.m, p)
			f.mu.Unlock()
		}
	}
	f := followFiles.m[path]
	if f == nil {
		f = &followFile{}
		followFiles.m[path] = f
	}
	f.lastUsed = now
	return f
}

// followSnapshot brings path's builder up to date and returns a copy of it.
func followSnapshot(path string, now time.Time) (*Follow, error) {
	f := getFollowFile(path, now)
	f.mu.Lock()
	defer f.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if f.b == nil || info.Size() < f.offset {
		f.b = newFollowBuilder()
		f.offset = 0
	}
	if info.Size() > f.offset || !info.ModTime().Equal(f.modTime) {
		n, err := consumeFrom(path, f.offset, f.b)
		if err != nil {
			return nil, err
		}
		f.offset += n
		f.modTime = info.ModTime()
	}
	return f.b.snapshot(), nil
}

// lineConsumer is fed one complete transcript line at a time.
type lineConsumer interface{ consume(line []byte) }

// consumeFrom feeds complete lines starting at offset to b and returns how
// many bytes it consumed. A trailing line without its newline is still being
// written; it is left for the next call.
func consumeFrom(path string, offset int64, b lineConsumer) (int64, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	if _, err := fh.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	r := bufio.NewReaderSize(fh, 256*1024)
	var consumed int64
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			if body := bytes.TrimSpace(line); len(body) > 0 && len(body) <= followMaxLineLen {
				b.consume(body)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return consumed, nil
			}
			return consumed, err
		}
	}
}

func buildFollow(path, sessionID string, pid *PIDInfo, now time.Time) (*Follow, error) {
	f, err := followSnapshot(path, now)
	if err != nil {
		return nil, err
	}
	f.SessionID = sessionID
	if f.CWD != "" {
		f.Project = filepath.Base(f.CWD)
	} else {
		f.Project = filepath.Base(filepath.Dir(path))
	}
	f.deriveState(pid, now)
	return f, nil
}

// FollowSessions returns the follow-along view of every running session that
// has a prompt, the one the user prompted most recently first: that one is
// what the window follows unless the user pins another.
func FollowSessions(claudeDir string) []*Follow {
	now := time.Now()
	pids := readPIDFiles(claudeDir)
	ids := make([]string, len(pids))
	for i := range pids {
		ids[i] = pids[i].SessionID
	}
	paths := findSessionFiles(claudeDir, ids)
	out := []*Follow{}
	for i := range pids {
		path := paths[pids[i].SessionID]
		if path == "" {
			continue // started, but nothing written yet
		}
		f, err := buildFollow(path, pids[i].SessionID, &pids[i], now)
		if err != nil || f.State == FollowEmpty {
			continue
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return promptedAt(out[i]).After(promptedAt(out[j])) })
	return out
}

// FollowSession returns the follow-along view of one session, running or
// not, or nil when no transcript has that id.
func FollowSession(claudeDir, sessionID string) (*Follow, error) {
	path := FindSessionFile(claudeDir, sessionID)
	if path == "" {
		return nil, nil
	}
	var pid *PIDInfo
	for _, p := range readPIDFiles(claudeDir) {
		if p.SessionID == sessionID {
			p := p
			pid = &p
			break
		}
	}
	return buildFollow(path, sessionID, pid, time.Now())
}

func promptedAt(f *Follow) time.Time {
	if f.LastPromptAt != nil {
		return *f.LastPromptAt
	}
	return f.LastActivity
}

// SessionIDForPath returns the session a transcript belongs to and its
// project directory name: the file's own name for a session file, the parent
// session for a subagent transcript (<project>/<session>/subagents/agent-*.jsonl).
func SessionIDForPath(path string) (sessionID, projectDir string) {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "subagents" {
		sessDir := filepath.Dir(dir)
		return filepath.Base(sessDir), filepath.Base(filepath.Dir(sessDir))
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl"), filepath.Base(dir)
}
