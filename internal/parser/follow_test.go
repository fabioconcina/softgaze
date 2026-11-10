package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Transcript line builders. Times are minutes after t0.

var t0 = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

func at(min float64) string {
	return t0.Add(time.Duration(min * float64(time.Minute))).Format(time.RFC3339Nano)
}

func line(v map[string]interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func human(min float64, text string) string {
	return line(map[string]interface{}{
		"type": "user", "timestamp": at(min), "cwd": "/repo", "origin": map[string]string{"kind": "human"},
		"message": map[string]interface{}{"role": "user", "content": text},
	})
}

func autoPrompt(min float64, text string) string {
	return line(map[string]interface{}{
		"type": "user", "timestamp": at(min), "origin": map[string]string{"kind": "task-notification"},
		"message": map[string]interface{}{"role": "user", "content": text},
	})
}

// queued is a command Claude Code absorbed into a turn already running:
// an attachment line, with no user line of its own.
func queued(min float64, mode, prompt string) string {
	return line(map[string]interface{}{
		"type": "attachment", "timestamp": at(min), "isSidechain": false,
		"attachment": map[string]interface{}{"type": "queued_command", "commandMode": mode, "prompt": prompt,
			"origin": map[string]string{"kind": mode}},
	})
}

func say(min float64, id, text, stop string) string {
	return line(map[string]interface{}{
		"type": "assistant", "timestamp": at(min),
		"message": map[string]interface{}{"id": id, "role": "assistant", "stop_reason": stop,
			"content": []map[string]interface{}{{"type": "text", "text": text}}},
	})
}

func use(min float64, id, name string, input map[string]interface{}) string {
	return line(map[string]interface{}{
		"type": "assistant", "timestamp": at(min),
		"message": map[string]interface{}{"id": "msg-" + id, "role": "assistant", "stop_reason": "tool_use",
			"content": []map[string]interface{}{{"type": "tool_use", "id": id, "name": name, "input": input}}},
	})
}

func result(min float64, id string) string {
	return line(map[string]interface{}{
		"type": "user", "timestamp": at(min), "toolUseResult": map[string]interface{}{"stdout": "ok"},
		"message": map[string]interface{}{"role": "user", "content": []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": id, "content": "ok"}}},
	})
}

func follow(t *testing.T, pid *PIDInfo, lines ...string) *Follow {
	t.Helper()
	b := newFollowBuilder()
	for _, l := range lines {
		b.consume([]byte(l))
	}
	f := b.snapshot()
	f.deriveState(pid, t0.Add(time.Hour))
	return f
}

func TestFollowWorkingNarration(t *testing.T) {
	f := follow(t, &PIDInfo{Status: "busy"},
		human(0, "add a health endpoint"),
		say(0.1, "m1", "I'll start by reading how the **routes** are registered.", "tool_use"),
		use(0.2, "r1", "Read", map[string]interface{}{"file_path": "/repo/server.go"}),
		result(0.3, "r1"),
		say(0.5, "m2", "Added the handler.", "tool_use"),
		use(0.6, "e1", "Edit", map[string]interface{}{"file_path": "/repo/health.go"}),
		result(0.7, "e1"),
		say(1, "m3", "Now let me run the tests and check the status code.\n\nDetails follow.", "tool_use"),
		use(1.1, "b1", "Bash", map[string]interface{}{"command": "go test ./..."}),
	)
	if f.State != FollowWorking || !f.Running {
		t.Fatalf("state = %s running=%v, want working", f.State, f.Running)
	}
	if f.Prompt != "add a health endpoint" || f.TurnStart == nil || !f.TurnStart.Equal(t0) {
		t.Errorf("prompt/start = %q %v", f.Prompt, f.TurnStart)
	}
	if f.Headline != "Now let me run the tests and check the status code.\n\nDetails follow." {
		t.Errorf("headline = %q", f.Headline)
	}
	if f.HeadlineAt == nil || !f.HeadlineAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("headline_at = %v, want the time of the latest sentence", f.HeadlineAt)
	}
	if f.Activity != "running go test ./..." || f.ActivitySince == nil {
		t.Errorf("activity = %q since %v", f.Activity, f.ActivitySince)
	}
}

// Claude goes from tool to tool without a word: between tools (thinking),
// Doing still says what it last set out to do.
func TestFollowSilentTools(t *testing.T) {
	lines := []string{
		human(0, "check the open PRs"),
		use(0.1, "b1", "Bash", map[string]interface{}{"command": "gh pr list", "description": "List open PRs on the website"}),
		result(0.2, "b1"),
		use(0.3, "w1", "Write", map[string]interface{}{"file_path": "/repo/notes.md"}),
		result(0.4, "w1"),
	}
	f := follow(t, &PIDInfo{Status: "busy"}, lines...)
	if f.Headline != "" || f.Activity != "thinking" || f.Doing != "writing notes.md" {
		t.Errorf("between tools: headline %q activity %q doing %q, want no headline, thinking, the last tool", f.Headline, f.Activity, f.Doing)
	}
	// Before any tool there is nothing to tell yet.
	if f := follow(t, &PIDInfo{Status: "busy"}, lines[0]); f.Doing != "" {
		t.Errorf("before any tool: doing %q, want empty", f.Doing)
	}
	// Only while Claude works.
	if f := follow(t, &PIDInfo{Status: "idle"}, append(lines, say(1, "m1", "Two PRs are open.", "end_turn"))...); f.Doing != "" {
		t.Errorf("finished: doing %q, want empty", f.Doing)
	}
	// A new turn starts with nothing done.
	if f := follow(t, &PIDInfo{Status: "busy"}, append(lines, say(1, "m1", "Done.", "end_turn"), human(2, "and the other site?"))...); f.Doing != "" {
		t.Errorf("new turn: doing %q, want empty", f.Doing)
	}
}

func TestFollowFinished(t *testing.T) {
	f := follow(t, &PIDInfo{Status: "idle"},
		human(0, "fix it"),
		say(0.1, "m1", "Looking at the failing test.", "tool_use"),
		use(0.2, "b1", "Bash", map[string]interface{}{"command": "go test ./..."}),
		result(1, "b1"),
		say(4, "m2", "Fixed the status code (500 -> 503).\n\n- All tests pass. Want me to document it in the README?", "end_turn"),
	)
	if f.State != FollowFinished || f.TurnEnd == nil || f.Activity != "" {
		t.Fatalf("state = %s end=%v activity=%q", f.State, f.TurnEnd, f.Activity)
	}
	if f.Headline != "Fixed the status code (500 -> 503).\n\n• All tests pass. Want me to document it in the README?" {
		t.Errorf("headline = %q, want the end of the final message", f.Headline)
	}
	if f.NeedsYou == nil || f.NeedsYou.Kind != "reply" || f.NeedsYou.Text != "Want me to document it in the README?" {
		t.Errorf("needs_you = %+v", f.NeedsYou)
	}
}

func TestFollowNeedsYou(t *testing.T) {
	running := []string{human(0, "run it"), use(0.1, "b1", "Bash", map[string]interface{}{"command": "kubectl rollout restart deploy/worker"})}
	tests := []struct {
		name  string
		lines []string
		pid   *PIDInfo
		want  *NeedsYou
	}{
		{"permission prompt", running, &PIDInfo{Status: "waiting", WaitingFor: "dialog open"},
			&NeedsYou{Kind: "approval", Text: "kubectl rollout restart deploy/worker"}},
		{"question", []string{human(0, "migrate"), use(0.1, "q1", "AskUserQuestion", map[string]interface{}{"questions": []map[string]interface{}{
			{"question": "Online backfill or a maintenance window?", "options": []map[string]string{{"label": "Online"}, {"label": "Window"}}}}})},
			&PIDInfo{Status: "busy"}, &NeedsYou{Kind: "question", Text: "Online backfill or a maintenance window?", Options: []string{"Online", "Window"}}},
		{"plan", []string{human(0, "plan it"), use(0.1, "p1", "ExitPlanMode", map[string]interface{}{})},
			&PIDInfo{Status: "busy"}, &NeedsYou{Kind: "plan", Text: "Approve or reject the plan"}},
		{"dialog with nothing pending", []string{human(0, "hi"), say(0.1, "m", "Hello.", "end_turn")},
			&PIDInfo{Status: "waiting", WaitingFor: "dialog open"}, &NeedsYou{Kind: "input", Text: "A dialog is open in Claude Code"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := follow(t, tt.pid, tt.lines...)
			if f.State != FollowNeedsYou || !reflect.DeepEqual(f.NeedsYou, tt.want) {
				t.Errorf("state = %s needs_you = %+v, want needs_you %+v", f.State, f.NeedsYou, tt.want)
			}
		})
	}
}

func TestFollowOtherStates(t *testing.T) {
	running := []string{human(0, "go"), use(0.1, "b1", "Bash", map[string]interface{}{"command": "make"})}
	tests := []struct {
		name string
		pid  *PIDInfo
		want string
	}{
		{"process gone", nil, FollowEnded},
		{"shell running", &PIDInfo{Status: "shell"}, FollowWorking},
		{"no status", &PIDInfo{}, FollowUnsure},
		{"a status softgaze does not know", &PIDInfo{Status: "compacting"}, FollowUnsure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if f := follow(t, tt.pid, running...); f.State != tt.want {
				t.Errorf("state = %s, want %s", f.State, tt.want)
			}
		})
	}
	if f := follow(t, &PIDInfo{Status: "idle"}); f.State != FollowEmpty {
		t.Errorf("no prompt: state = %s, want empty", f.State)
	}
}

// Unsure: never a confident color softgaze cannot back.
func TestFollowUnsure(t *testing.T) {
	mid := []string{
		human(0, "go"),
		say(0.1, "m1", "Reading the code.", "tool_use"),
		use(0.2, "r1", "Read", map[string]interface{}{"file_path": "/repo/a.go"}),
		result(0.3, "r1"),
		say(0.4, "m2", "Now the tests.\n\nThen the docs.", "tool_use"),
		use(0.5, "b1", "Bash", map[string]interface{}{"command": "go test ./..."}),
	}
	f := follow(t, &PIDInfo{Status: "compacting"}, mid...)
	if f.State != FollowUnsure || !strings.Contains(f.Unsure, `"compacting"`) {
		t.Fatalf("unknown status: state = %s unsure = %q", f.State, f.Unsure)
	}
	if f.Headline != "Now the tests.\n\nThen the docs." || len(f.Trail) == 0 || f.NeedsYou != nil {
		t.Errorf("unknown status mid-turn: headline %q trail %v needs_you %+v, want the latest narration and no reply", f.Headline, f.Trail, f.NeedsYou)
	}
	done := append(mid[:4:4], say(2, "m3", "All done.\n\nShall I commit?", "end_turn"))
	if f := follow(t, &PIDInfo{}, done...); f.State != FollowUnsure || f.Unsure == "" || f.Headline != "All done.\n\nShall I commit?" || f.NeedsYou != nil {
		t.Errorf("no status after the turn: %s %q headline %q needs_you %+v", f.State, f.Unsure, f.Headline, f.NeedsYou)
	}
	// A pending question is plain in the transcript, whatever the status.
	q := []string{human(0, "migrate"), use(0.1, "q1", "AskUserQuestion", map[string]interface{}{"questions": []map[string]interface{}{
		{"question": "Online or offline?", "options": []map[string]string{{"label": "Online"}, {"label": "Offline"}}}}})}
	if f := follow(t, &PIDInfo{Status: "compacting"}, q...); f.State != FollowNeedsYou {
		t.Errorf("question with an unknown status: %s, want needs_you", f.State)
	}

	// Busy without a prompt the parser can read: past the grace, unsure;
	// within it, or in a shell or a dialog, just no prompt yet.
	now := t0.Add(time.Hour)
	unread := []string{line(map[string]interface{}{"type": "user", "timestamp": at(59), "message": map[string]interface{}{"role": "user", "contents": "renamed field"}})}
	for _, c := range []struct {
		name string
		pid  *PIDInfo
		want string
	}{
		{"busy past the grace", &PIDInfo{Status: "busy", StatusUpdatedAt: now.Add(-time.Minute).UnixMilli()}, FollowUnsure},
		{"busy within the grace", &PIDInfo{Status: "busy", StatusUpdatedAt: now.Add(-10 * time.Second).UnixMilli()}, FollowEmpty},
		{"busy, no status time", &PIDInfo{Status: "busy"}, FollowEmpty},
		{"shell before any prompt", &PIDInfo{Status: "shell", StatusUpdatedAt: now.Add(-time.Minute).UnixMilli()}, FollowEmpty},
		{"dialog before any prompt", &PIDInfo{Status: "waiting", StatusUpdatedAt: now.Add(-time.Minute).UnixMilli()}, FollowEmpty},
	} {
		if f := follow(t, c.pid, unread...); f.State != c.want {
			t.Errorf("%s: state = %s, want %s", c.name, f.State, c.want)
		}
	}
}

func TestFollowTurnBoundaries(t *testing.T) {
	f := follow(t, &PIDInfo{Status: "idle"},
		human(0, "first"),
		say(0.5, "m1", "On it.", "tool_use"),
		human(1, "also do this"), // typed mid-turn: same turn
		say(2, "m2", "Done with both.", "end_turn"),
		autoPrompt(3, "<task-notification>build finished</task-notification>"), // Claude resumes the turn
		say(3.5, "m3", "The build is green.", "end_turn"),
		human(10, "/model"), // a local command Claude never answers
	)
	if f.Prompt != "first" || f.Headline != "The build is green." {
		t.Errorf("prompt/headline = %q / %q, want the first turn kept", f.Prompt, f.Headline)
	}
	if f.LastPromptAt == nil || !f.LastPromptAt.Equal(t0.Add(10*time.Minute)) {
		t.Errorf("last prompt = %v, want the /model time", f.LastPromptAt)
	}

	f = follow(t, &PIDInfo{Status: "idle"},
		human(0, "first"), say(1, "m1", "Done.", "end_turn"),
		human(5, "second"), say(5.5, "m2", "Starting.", "tool_use"),
		human(6, "[Request interrupted by user]"),
	)
	if f.Prompt != "second" || !f.Interrupted || f.State != FollowFinished || f.NeedsYou != nil {
		t.Errorf("after interrupt: %+v", f)
	}
}

func TestFollowIncremental(t *testing.T) {
	lines := []string{
		human(0, "go"),
		say(0.1, "m1", "Reading the code.", "tool_use"),
		use(0.2, "r1", "Read", map[string]interface{}{"file_path": "/repo/a.go"}),
		result(0.3, "r1"),
		say(0.4, "m2", "Running the tests.", "tool_use"),
		use(0.5, "b1", "Bash", map[string]interface{}{"command": "go test ./..."}),
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo := func(s string) {
		fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		fh.WriteString(s)
		fh.Close()
	}
	// The first lines, plus half a line still being written.
	appendTo(strings.Join(lines[:4], "\n") + "\n" + lines[4][:25])
	part, err := buildFollow(path, "s", &PIDInfo{Status: "busy"}, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if part.Headline != "Reading the code." || part.Activity != "thinking" {
		t.Fatalf("partial: headline %q activity %q", part.Headline, part.Activity)
	}
	appendTo(lines[4][25:] + "\n" + lines[5] + "\n")
	got, _ := buildFollow(path, "s", &PIDInfo{Status: "busy"}, t0.Add(time.Minute))
	if got.Headline != "Running the tests." || got.Activity != "running go test ./..." {
		t.Errorf("after append: headline %q activity %q", got.Headline, got.Activity)
	}
	// A rewritten (shorter) file is parsed again from the start.
	os.WriteFile(path, []byte(human(5, "fresh")+"\n"), 0o644)
	again, _ := buildFollow(path, "s", &PIDInfo{Status: "busy"}, t0.Add(6*time.Minute))
	if again.Prompt != "fresh" || again.Headline != "" {
		t.Errorf("after rewrite: %+v", again)
	}
}

func TestFollowSessionsLatestPromptFirst(t *testing.T) {
	claudeDir := t.TempDir()
	proj := filepath.Join(claudeDir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	os.MkdirAll(filepath.Join(claudeDir, "sessions"), 0o755)
	write := func(id string, prompted float64, pid int) {
		os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(human(prompted, "task "+id)+"\n"), 0o644)
		info, _ := json.Marshal(PIDInfo{PID: pid, SessionID: id, CWD: "/repo", Status: "busy"})
		os.WriteFile(filepath.Join(claudeDir, "sessions", strconv.Itoa(pid)+".json"), info, 0o644)
	}
	// Two live processes stand in for two running sessions.
	write("older", 1, os.Getpid())
	write("newer", 5, os.Getppid())
	os.WriteFile(filepath.Join(proj, "closed.jsonl"), []byte(human(9, "no process")+"\n"), 0o644)

	running := FollowSessions(claudeDir)
	if len(running) != 2 {
		t.Fatalf("FollowSessions = %d sessions, want the 2 running ones", len(running))
	}
	if f := running[0]; f.SessionID != "newer" || f.Prompt != "task newer" || !f.Running {
		t.Errorf("first = %q (%q), want the session prompted last", f.SessionID, f.Prompt)
	}
	if running[1].SessionID != "older" {
		t.Errorf("second = %q, want older", running[1].SessionID)
	}
	if f, _ := FollowSession(claudeDir, "closed"); f == nil || f.State != FollowEnded {
		t.Errorf("FollowSession(closed) = %+v, want an ended session", f)
	}
	if f, _ := FollowSession(claudeDir, "older"); f == nil || !f.Running {
		t.Errorf("FollowSession(older) = %+v, want it running", f)
	}
	if f, _ := FollowSession(claudeDir, "missing"); f != nil {
		t.Errorf("FollowSession(missing) = %+v, want nil", f)
	}
	if got := FollowSessions(t.TempDir()); len(got) != 0 {
		t.Errorf("FollowSessions(empty dir) = %v, want none", got)
	}
}

func TestHeadAndTailText(t *testing.T) {
	long := strings.Repeat("word ", 60) // 300 bytes, no sentence end
	tests := []struct {
		name, got, want string
	}{
		{"head: whole lines", headText("First line.\n\nSecond paragraph.", 240), "First line.\n\nSecond paragraph."},
		{"head: lines while they fit", headText("First line.\n\nA second paragraph that does not fit.", 20), "First line. …"},
		{"head: list kept", headText("Here's the plan:\n\n- **Between:** a line\n- Under: another\n\n```go\ncode()\n```", 240), "Here's the plan:\n\n• Between: a line\n• Under: another"},
		{"head: steps and nesting", headText("Do this:\n1. Build\n   - with tests\n     - race on\n2) Ship\nDone.", 240), "Do this:\n1. Build\n  • with tests\n    • race on\n2. Ship\nDone."},
		{"head: table rule dropped", headText("| a | b |\n|---|---|\n| 1 | 2 |", 240), "| a | b |\n| 1 | 2 |"},
		{"head: markdown flattened", headText("- **Now** run [the tests](http://x) with `go test`", 240), "• Now run the tests with go test"},
		{"head: cut at a sentence", headText("Short one. "+strings.Repeat("x", 150)+". Then more text that goes past the cap here.", 200), "Short one. " + strings.Repeat("x", 150) + "."},
		{"head: cut at a word", headText(long, 20), "word word word word…"},
		{"tail: whole lines", tailText("Intro.\n\nAll done. Push it?", 240), "Intro.\n\nAll done. Push it?"},
		{"tail: last lines while they fit", tailText("A first paragraph that is long.\n\nAll done. Push it?", 25), "…All done. Push it?"},
		{"tail: starts at a sentence", tailText("A long first sentence that will not fit at all. Then the end.", 30), "…Then the end."},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestTrailingQuestion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"All tests pass. Want me to document it in the README?", "Want me to document it in the README?"},
		{"Done.\n\n- Shall I **push** it?", "Shall I push it?"},
		{"Done. Nothing else to do.", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := trailingQuestion(tt.in); got != tt.want {
			t.Errorf("trailingQuestion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPromptText(t *testing.T) {
	tests := []struct {
		name   string
		blocks []contentBlock
		want   string
	}{
		{"ide context removed", []contentBlock{
			{Type: "text", Text: "<ide_opened_file>The user opened a.go</ide_opened_file>"},
			{Type: "text", Text: "fix it"},
		}, "fix it"},
		{"system reminder removed", []contentBlock{{Type: "text", Text: "do it\n<system-reminder>\nnoise\n</system-reminder>"}}, "do it"},
		{"slash command", []contentBlock{{Type: "text", Text: "<command-name>/verify</command-name>\n<command-args>fast</command-args>"}}, "/verify fast"},
		{"image", []contentBlock{{Type: "text", Text: "look"}, {Type: "image"}}, "look [image]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := promptText(tt.blocks); got != tt.want {
				t.Errorf("promptText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionIDForPath(t *testing.T) {
	sub := filepath.Join("projects", "proj", "sess", "subagents", "agent-x.jsonl")
	if id, dir := SessionIDForPath(sub); id != "sess" || dir != "proj" {
		t.Errorf("subagent transcript -> %s, %s", id, dir)
	}
	if id, dir := SessionIDForPath(filepath.Join("projects", "proj", "abc.jsonl")); id != "abc" || dir != "proj" {
		t.Errorf("session transcript -> %s, %s", id, dir)
	}
}

func TestDescribeTool(t *testing.T) {
	tests := []struct {
		name, tool            string
		in                    toolInput
		wantActivity, wantSum string
	}{
		{"described command", "Bash", toolInput{Command: "go test ./...\necho done", Description: "Run the test suite"}, "Run the test suite", "go test ./... ..."},
		{"bare command", "PowerShell", toolInput{Command: "Get-Process"}, "running Get-Process", "Get-Process"},
		{"edit", "Edit", toolInput{FilePath: filepath.Join("repo", "health.go")}, "editing health.go", "edit " + filepath.Join("repo", "health.go")},
		{"subagent", "Agent", toolInput{Description: "refactor the parser"}, "subagent: refactor the parser", "refactor the parser"},
		{"mcp tool", "mcp__sonarqube__show_rule", toolInput{}, "using sonarqube (show_rule)", "mcp__sonarqube__show_rule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, s := describeTool(tt.tool, tt.in)
			if a != tt.wantActivity || s != tt.wantSum {
				t.Errorf("describeTool = %q, %q; want %q, %q", a, s, tt.wantActivity, tt.wantSum)
			}
		})
	}
}

func TestFollowTaskSkipsShortReplies(t *testing.T) {
	f := follow(t, &PIDInfo{Status: "busy"},
		human(0, "Add a health endpoint that checks the database"),
		say(1, "m1", "Should I also add a test?", "end_turn"),
		human(2, "yes"),
		say(2.1, "m2", "Writing the test.", "tool_use"),
	)
	if f.Prompt != "yes" || f.Task != "Add a health endpoint that checks the database" {
		t.Errorf("prompt/task = %q / %q, want the reply and the request before it", f.Prompt, f.Task)
	}
	f = follow(t, &PIDInfo{Status: "busy"}, human(0, "ok"), say(0.1, "m", "On it.", "tool_use"))
	if f.Task != "ok" {
		t.Errorf("task = %q, want the prompt when there is no earlier request", f.Task)
	}
}

func TestFollowBackgroundWork(t *testing.T) {
	launch := line(map[string]interface{}{
		"type": "assistant", "timestamp": at(1),
		"message": map[string]interface{}{"id": "m-bg", "role": "assistant", "stop_reason": "tool_use",
			"content": []map[string]interface{}{{"type": "tool_use", "id": "bg1", "name": "Bash",
				"input": map[string]interface{}{"command": "python swap.py", "description": "Re-run the background swap", "run_in_background": true}}}},
	})
	launched := line(map[string]interface{}{
		"type": "user", "timestamp": at(1.1), "toolUseResult": map[string]interface{}{"backgroundTaskId": "bx1"},
		"message": map[string]interface{}{"role": "user", "content": []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "bg1", "content": "Command running in background with ID: bx1. Output is being written to: x"}}},
	})
	lines := []string{
		human(0, "fix the white balance and re-run the swap"),
		launch, launched,
		say(2, "m2", "The swap is re-running and will write picks-glass/ in about 15 minutes. When it's done I'll show you a sheet.", "end_turn"),
	}
	f := follow(t, &PIDInfo{Status: "idle"}, lines...)
	if f.State != FollowBackground || f.Background == nil || f.Background.What != "Re-run the background swap" || f.Background.Count != 1 {
		t.Fatalf("state = %s background = %+v, want background with the swap", f.State, f.Background)
	}
	if f.NeedsYou != nil {
		t.Errorf("needs_you = %+v, want none while Claude waits on its own work", f.NeedsYou)
	}
	// The task reports back; Claude says it is done: now it is your turn.
	done := append(lines,
		autoPrompt(16, "<task-notification>\n<task-id>bx1</task-id>\n<tool-use-id>bg1</tool-use-id>\n<status>completed</status>\n</task-notification>"),
		say(17, "m3", "Here is the sheet of all 9 people.", "end_turn"))
	if f := follow(t, &PIDInfo{Status: "idle"}, done...); f.State != FollowFinished || f.Background != nil {
		t.Errorf("after the notice: state = %s background = %+v, want finished", f.State, f.Background)
	}
	// The task reports back while Claude is still at work: Claude Code
	// absorbs the notice into the running turn as an attachment, with no
	// user line. Once Claude ends the turn it is still your turn.
	absorbed := []string{
		lines[0], launch, launched,
		use(3, "r1", "Read", map[string]interface{}{"file_path": "/repo/swap.py"}),
		queued(4, "task-notification", "<task-notification>\n<task-id>bx1</task-id>\n<tool-use-id>bg1</tool-use-id>\n<status>completed</status>\n</task-notification>"),
		result(4.1, "r1"),
		say(5, "m5", "The swap is done and the sheet is ready.", "end_turn"),
	}
	if f := follow(t, &PIDInfo{Status: "idle"}, absorbed...); f.State != FollowFinished || f.Background != nil {
		t.Errorf("notice absorbed mid-turn: state = %s background = %+v, want finished", f.State, f.Background)
	}
	// Started before this Claude Code process (a resumed session): gone.
	if f := follow(t, &PIDInfo{Status: "idle", StartedAt: t0.Add(5 * time.Minute).UnixMilli()}, lines...); f.State != FollowFinished {
		t.Errorf("task from an earlier process: state = %s, want finished", f.State)
	}
	// Started in an earlier turn that never reports (a dev server): once the
	// user asks something else and Claude answers, it is their turn.
	later := append(lines,
		human(30, "anything else we can fold? just discuss"),
		say(31, "m4", "Two more ideas, both optional.", "end_turn"))
	if f := follow(t, &PIDInfo{Status: "idle"}, later...); f.State != FollowFinished || f.Background != nil {
		t.Errorf("task from an earlier turn: state = %s background = %+v, want finished", f.State, f.Background)
	}
}

func TestSplitQuestion(t *testing.T) {
	long := `The tab still answers two questions on one page: "how is each installation?" (the table) and "what does each service upgrade fix?" (the 860 px releases table), with nothing saying which comes first. Which direction should I build?`
	cases := []struct{ in, q, ctx string }{
		{long, "Which direction should I build?", `The tab still answers two questions on one page: "how is each installation?" (the table) and "what does each service upgrade fix?" (the 860 px releases table), with nothing saying which comes first.`},
		{"Which auth method should we use?", "Which auth method should we use?", ""},
		{strings.Repeat("A long question with no sentence break in it at all ", 4) + "?", strings.Repeat("A long question with no sentence break in it at all ", 4) + "?", ""},
	}
	for _, c := range cases {
		q, ctx := splitQuestion(c.in)
		if q != strings.TrimSpace(c.q) || ctx != c.ctx {
			t.Errorf("splitQuestion(%.30q) = %q, %q; want %q, %q", c.in, q, ctx, c.q, c.ctx)
		}
	}
}

func TestFollowTrail(t *testing.T) {
	read := map[string]interface{}{"file_path": "/repo/server.go"}
	lines := []string{
		human(0, "add a health endpoint to the server"),
		use(0.05, "g0", "Grep", map[string]interface{}{"pattern": "routes"}),
		say(0.1, "m1", "I'll start by reading how the routes are registered.", "tool_use"),
		use(0.11, "r1", "Read", read), use(0.12, "r2", "Read", read), use(0.13, "e1", "mcp__ide__getDiagnostics", nil),
		say(0.2, "m2", "Routes live in server.go; adding the handler there.", "tool_use"),
		use(0.21, "w1", "Edit", read),
		say(0.3, "m3", "Now running the tests.", "tool_use"),
		use(0.31, "b1", "Bash", map[string]interface{}{"command": "go test"}),
		use(0.32, "b2", "Bash", map[string]interface{}{"command": "go vet"}),
	}
	f := follow(t, &PIDInfo{Status: "busy"}, lines...)
	want := []TrailItem{
		{"tools", "Grep"},
		{"text", "I'll start by reading how the routes are registered."},
		{"tools", "Read ×2 · getDiagnostics"},
		{"text", "Routes live in server.go; adding the handler there."},
		{"tools", "Edit"},
	}
	if f.Headline != "Now running the tests." || !reflect.DeepEqual(f.Trail, want) || f.Tools != "Bash ×2" {
		t.Fatalf("working: headline %q tools %q trail %+v, want trail %+v and tools Bash ×2", f.Headline, f.Tools, f.Trail, want)
	}
	// Finished: the final message fits whole in the headline; the trail is
	// what came before it.
	done := append(lines, say(0.5, "m4", "Added GET /health returning 200.\n\nWant me to open a PR?", "end_turn"))
	f = follow(t, &PIDInfo{Status: "idle"}, done...)
	if f.Headline != "Added GET /health returning 200.\n\nWant me to open a PR?" || f.Tools != "" {
		t.Fatalf("finished: headline %q tools %q", f.Headline, f.Tools)
	}
	if n := len(f.Trail); n < 2 || f.Trail[n-1] != (TrailItem{"tools", "Bash ×2"}) || f.Trail[n-2] != (TrailItem{"text", "Now running the tests."}) {
		t.Fatalf("finished: trail %+v, want it to end with the last message before and its tools", f.Trail)
	}
	// A long turn keeps its last followTrailMax messages before the headline.
	many := []string{human(0, "add a health endpoint to the server")}
	for i := 0; i < followTrailMax+4; i++ {
		many = append(many, say(0.1+float64(i)*0.001, fmt.Sprintf("n%d", i), fmt.Sprintf("Step %d done.", i), "tool_use"))
	}
	f = follow(t, &PIDInfo{Status: "busy"}, many...)
	if len(f.Trail) != followTrailMax || f.Trail[0].Text != "Step 3 done." || f.Trail[followTrailMax-1].Text != fmt.Sprintf("Step %d done.", followTrailMax+2) || f.Headline != fmt.Sprintf("Step %d done.", followTrailMax+3) {
		t.Errorf("long turn: headline %q trail %+v, want the last followTrailMax steps before the headline", f.Headline, f.Trail)
	}
	// No earlier text: an empty list, not null.
	f = follow(t, &PIDInfo{Status: "busy"}, human(0, "add a health endpoint to the server"))
	if f.Trail == nil || len(f.Trail) != 0 {
		t.Errorf("no narration: trail = %#v, want []", f.Trail)
	}
}

func TestFollowNarrationBlocks(t *testing.T) {
	block := func(min float64, id, kind, sig, text string) string {
		return line(map[string]interface{}{
			"type": "assistant", "timestamp": at(min),
			"message": map[string]interface{}{"id": id, "role": "assistant", "stop_reason": "tool_use",
				"content": []map[string]interface{}{{"type": "thinking", "thinking": text, "signature": sig}}},
		})
	}
	const narration = "CAQSvwYKEQgTGAI4AUIJbmFycmF0aW9uEgz6yz3o/oV5gvjA3VEaDFuAGeEbkGOMqI3WCyIw"
	const thinking = "CAQS2woKEAgTGAI4AUIIdGhpbmtpbmcSDFlz/83ZnvbvW58yexoMMs0gq26E5yS8nAEdIjAL"
	f := follow(t, &PIDInfo{Status: "busy"},
		human(0, "show the tool calls in the trail"),
		say(0.1, "m1", "Both rules now match.", "tool_use"),
		use(0.2, "b1", "Bash", map[string]interface{}{"command": "go build"}),
		block(0.3, "m2", "thinking", thinking, "private reasoning that must never show"),
		block(0.31, "m2", "narration", narration, "The server sends the tools line."),
	)
	if f.Headline != "The server sends the tools line." {
		t.Fatalf("headline = %q, want the narration block", f.Headline)
	}
	for _, it := range f.Trail {
		if strings.Contains(it.Text, "private") {
			t.Fatalf("thinking leaked into the trail: %+v", f.Trail)
		}
	}
	if !isNarration(narration) || isNarration(thinking) || isNarration("") || isNarration("!!not base64!!") {
		t.Error("isNarration misjudges a signature")
	}
	other := base64.StdEncoding.EncodeToString([]byte("\x08\x04\x12\x10header: summary block, then the rest"))
	for sig, want := range map[string]string{narration: "narration", thinking: "thinking", other: "", "": "", "!!not base64!!": ""} {
		if got := signatureKind(sig); got != want {
			t.Errorf("signatureKind(%.20q) = %q, want %q", sig, got, want)
		}
	}
}

func TestFollowLongFinalAnswer(t *testing.T) {
	var paras []string
	for i := 0; i < 30; i++ {
		paras = append(paras, fmt.Sprintf("Paragraph %d explains one part of the change in a full sentence.", i))
	}
	long := strings.Join(paras, "\n\n") // about 2,000 bytes
	f := follow(t, &PIDInfo{Status: "idle"},
		human(0, "explain the whole change"),
		say(1, "m1", long, "end_turn"),
	)
	if !strings.HasPrefix(f.Headline, "Paragraph 0 ") || !strings.HasSuffix(f.Headline, "Paragraph 29 explains one part of the change in a full sentence.") {
		t.Fatalf("headline is cut: %.80q ... (%d bytes)", f.Headline, len(f.Headline))
	}
}
