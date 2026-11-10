package parser

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Follow-along: a one-glance view of what Claude is doing in the session you
// prompted last, for the floating window that keeps you with the task while
// Claude works. Claude narrates its work in short sentences between tool calls
// (in nearly every long turn); the latest one is the headline, the current
// tool call is the activity line. Built deterministically from the
// transcript, incrementally, and only when a client asks.

// Session states reported in Follow.State.
const (
	FollowWorking  = "working"   // Claude is running a turn
	FollowNeedsYou = "needs_you" // blocked on the user: a question, an approval, a dialog
	FollowFinished = "finished"  // turn over, process still alive
	// Turn over, but Claude started work in the background and will carry
	// on when it reports: not the user's turn yet.
	FollowBackground = "background"
	FollowEnded      = "ended" // process gone
	FollowEmpty      = "empty" // no prompt yet
	// softgaze cannot tell: Claude Code reports a status it does not
	// know, or none, or says it is busy while the transcript shows no prompt
	// it can read (its format changed). Better said than a confident wrong
	// color; Follow.Unsure says why.
	FollowUnsure = "unsure"
)

// The PID statuses softgaze knows (see PIDInfo.Status).
var knownStatus = map[string]bool{"busy": true, "shell": true, "idle": true, "waiting": true}

var loggedStatus sync.Map

// logUnknownStatus notes a status softgaze does not know, once per
// value: the first sign that Claude Code changed what it writes.
func logUnknownStatus(s string) {
	if _, seen := loggedStatus.LoadOrStore(s, true); !seen {
		log.Printf("[follow] Claude Code reports an unknown session status %q: showing \"Can't tell\"", s)
	}
}

// followUnsureGrace: how long Claude Code may say it is busy before its
// first prompt shows up in the transcript.
const followUnsureGrace = 30 * time.Second

const (
	followHeadlineMax = 700 // bytes of the headline: the window shows what fits
	// followFinalMax: the final message is kept whole (the window scrolls it).
	followFinalMax  = followTextMax
	followTrailMax  = 30   // messages before the headline (the window shows what fits)
	followTrailItem = 200  // bytes of each
	followTextMax   = 8000 // bytes of a text block kept while parsing
	followPromptMax = 300  // bytes of the prompt shown

	// A prompt typed while Claude is mid-turn is a follow-up to that turn,
	// unless the turn has been silent this long (it probably died).
	followFollowUpGap = 10 * time.Minute
)

// Follow is the follow-along view of one session.
type Follow struct {
	SessionID string `json:"session_id"`
	Project   string `json:"project"`
	CWD       string `json:"cwd"`
	Running   bool   `json:"running"`
	State     string `json:"state"`

	Prompt string `json:"prompt"`
	// Task is the last real request, for keeping the goal in sight: the
	// prompt itself, or the one before it when the prompt is a short reply
	// ("yes", "1", "do it").
	Task        string     `json:"task"`
	TurnStart   *time.Time `json:"turn_start,omitempty"`
	TurnEnd     *time.Time `json:"turn_end,omitempty"`
	Interrupted bool       `json:"interrupted,omitempty"`

	// Headline is Claude's latest sentence while it works, or the end of its
	// final message once it stops.
	Headline   string     `json:"headline"`
	HeadlineAt *time.Time `json:"headline_at,omitempty"`
	// Trail is what came before the headline in this turn, oldest first:
	// Claude's earlier messages (at most followTrailMax; how each began while
	// it works, how each ended once it stops, then the start of its final
	// message), each followed by the tools it used before the next one.
	Trail []TrailItem `json:"trail"`
	// Tools are the tools Claude used since the headline, by name only
	// ("Read ×3 · Edit · Bash"); empty when none.
	Tools string `json:"tools,omitempty"`

	Activity      string     `json:"activity,omitempty"`
	ActivitySince *time.Time `json:"activity_since,omitempty"`
	NeedsYou      *NeedsYou  `json:"needs_you,omitempty"`
	// Doing is what Claude last set out to do with a tool in this turn, in
	// the words it gave the tool, while it works: between tools, when it has
	// said nothing yet, that is still the best account of where it is.
	Doing string `json:"doing,omitempty"`
	// Background is the work Claude is waiting on, in the background state.
	Background *Background `json:"background,omitempty"`
	// Unsure is why softgaze cannot tell what the session is doing, in
	// the unsure state.
	Unsure string `json:"unsure,omitempty"`
	// AtItSince is when the user's current run of work began (no pause of
	// 15 minutes since), set by the server only at their turn and after 90
	// minutes or more: the window then suggests a break.
	AtItSince *time.Time `json:"at_it_since,omitempty"`
	// Entrypoint is where the session runs, from its PID file:
	// "claude-vscode" for the VS Code extension, "cli" for a terminal.
	Entrypoint string `json:"entrypoint,omitempty"`

	LastActivity time.Time  `json:"last_activity"`
	LastPromptAt *time.Time `json:"last_prompt_at,omitempty"`

	// Focus is the user's current stretch on this session (see focus.go),
	// when it is still going; set by the HTTP handler.
	Focus *FocusStretch `json:"focus,omitempty"`

	pending  []pendingCall // tool calls of the latest turn awaiting a result
	latest   *narrated     // the latest turn's newest narration
	trail    []narrated    // the messages before it, oldest first
	preTools []toolCount   // tools used before the turn's first message
	doing    string        // the latest tool call's activity in the turn
	bg       []bgTask      // background work not reported back yet
	mid      bool          // the turn is under way: the latest narration leads
}

// Background describes the work a background state waits on: the most
// recent task, and how many are running.
type Background struct {
	What  string    `json:"what"`
	Since time.Time `json:"since"`
	Count int       `json:"count"`
}

// NeedsYou says what the session is waiting for.
type NeedsYou struct {
	Kind    string   `json:"kind"` // question | plan | approval | input | reply
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
	// Context is what Claude wrote before a long question's last sentence,
	// which is Text: the question itself comes first in a small window.
	Context string `json:"context,omitempty"`
}

type narrated struct {
	at    time.Time
	text  string
	tools []toolCount // the tools used after it, before the next message
}

// TrailItem is one line of Follow.Trail: a message ("text") or the tools
// used after it ("tools").
type TrailItem struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// toolCount is how often a tool was used in a row of calls, in the order
// the tools first came up.
type toolCount struct {
	name string
	n    int
}

// followToolKinds caps the distinct tools kept per message.
const followToolKinds = 12

func addTool(list []toolCount, name string) []toolCount {
	if i := strings.LastIndex(name, "__"); strings.HasPrefix(name, "mcp__") && i >= 0 {
		name = name[i+2:] // an MCP tool by its own name
	}
	for i := range list {
		if list[i].name == name {
			list[i].n++
			return list
		}
	}
	if len(list) >= followToolKinds {
		return list
	}
	return append(list, toolCount{name: name, n: 1})
}

// toolsText lists tools by name, with a count when used more than once.
func toolsText(list []toolCount) string {
	parts := make([]string, 0, len(list))
	for _, t := range list {
		if t.n > 1 {
			parts = append(parts, t.name+" ×"+strconv.Itoa(t.n))
		} else {
			parts = append(parts, t.name)
		}
	}
	return strings.Join(parts, " · ")
}

type pendingCall struct {
	name     string
	activity string
	summary  string
	since    time.Time
	question *NeedsYou
}

// followEntry is the subset of a transcript line the follow view needs.
type followEntry struct {
	Type             string    `json:"type"`
	Timestamp        time.Time `json:"timestamp"`
	CWD              string    `json:"cwd"`
	IsMeta           bool      `json:"isMeta"`
	IsCompactSummary bool      `json:"isCompactSummary"`
	IsSidechain      bool      `json:"isSidechain"`
	TurnOrigin       string    `json:"turnOrigin"`
	Origin           *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message *struct {
		ID         string          `json:"id"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	// Attachment is what Claude Code delivers into a turn already running
	// (type "attachment"), without a user line: a queued_command is a
	// background task's notice (commandMode "task-notification") or a
	// prompt typed mid-turn ("prompt").
	Attachment *struct {
		Type        string          `json:"type"`
		CommandMode string          `json:"commandMode"`
		Prompt      json.RawMessage `json:"prompt"`
	} `json:"attachment"`
}

// followBuilder folds transcript lines into the latest turn. It is fed
// incrementally (see followcache.go), so a live session costs only its new
// lines.
type followBuilder struct {
	cwd          string
	turns        []*followTurn // the latest turn, plus the one before it
	lastActivity time.Time
	lastPrompt   time.Time
	task         string // the last prompt that reads like a request
	inTurn       bool
	calls        map[string]*toolCall
	background   map[string]*bgTask // by the tool-use ID that started it
	lastTextMsg  string
}

type followTurn struct {
	prompt       string
	start        time.Time
	lastActivity time.Time
	end          *time.Time
	interrupted  bool
	local        bool        // a slash command; dropped if Claude never acts on it
	active       bool        // Claude did something in this turn
	latest       *narrated   // newest text Claude wrote in this turn
	trail        []*narrated // the messages before latest, oldest first
	preTools     []toolCount // tools used before Claude's first message
	doing        string      // the latest tool call's activity
}

type toolCall struct {
	name     string
	activity string
	summary  string
	at       time.Time
	turn     *followTurn
	question *NeedsYou
}

func newFollowBuilder() *followBuilder {
	return &followBuilder{calls: map[string]*toolCall{}, background: map[string]*bgTask{}}
}

// bgTask is work Claude started in the background (a shell, an agent) and
// will come back to when it reports: until then a finished turn is not the
// user's turn.
type bgTask struct {
	what   string // what it runs, in plain words
	since  time.Time
	taskID string // the ID Claude Code gave it, from the launch result
}

var (
	bgLaunchRe = regexp.MustCompile(`running in background with ID: ([A-Za-z0-9_-]+)`)
	// Completion notices name the task and the call that started it.
	bgNoticeRe = regexp.MustCompile(`<(?:task-id|tool-use-id)>([A-Za-z0-9_-]+)</`)
)

// followBgMax: background work older than this is assumed gone (a missed
// notice, a restarted Claude Code).
const followBgMax = 12 * time.Hour

// endBackground forgets the task with this tool-use ID or task ID.
func (b *followBuilder) endBackground(id string) {
	for k, t := range b.background {
		if k == id || t.taskID == id {
			delete(b.background, k)
		}
	}
}

func (b *followBuilder) consume(line []byte) {
	var e followEntry
	if err := json.Unmarshal(line, &e); err != nil {
		return
	}
	// The launch folder, not the latest: Claude's shell can cd elsewhere.
	if e.CWD != "" && b.cwd == "" {
		b.cwd = e.CWD
	}
	if e.Type == "attachment" && !e.IsSidechain {
		b.onAttachment(&e)
		return
	}
	if e.Message == nil || e.Timestamp.IsZero() || e.IsSidechain {
		return
	}
	switch e.Type {
	case "assistant":
		b.onAssistant(&e)
	case "user":
		b.onUser(&e)
	}
}

// touch advances the clocks. Compaction summaries and replayed lines carry
// older timestamps, so they only move forward.
func (b *followBuilder) touch(t *followTurn, ts time.Time) {
	if ts.After(b.lastActivity) {
		b.lastActivity = ts
	}
	if t != nil && ts.After(t.lastActivity) {
		t.lastActivity = ts
	}
}

func (b *followBuilder) current() *followTurn {
	if len(b.turns) == 0 {
		return nil
	}
	return b.turns[len(b.turns)-1]
}

func (b *followBuilder) onAssistant(e *followEntry) {
	t := b.current()
	if t == nil {
		// Claude acted before any prompt we recognized (a resumed session).
		t = &followTurn{start: e.Timestamp, lastActivity: e.Timestamp}
		b.turns = append(b.turns, t)
	}
	b.touch(t, e.Timestamp)
	t.active = true
	if t.end != nil && e.Timestamp.After(*t.end) {
		t.end = nil // Claude picked the turn up again
	}
	for _, blk := range parseContentBlocks(e.Message.Content) {
		switch blk.Type {
		case "text", "thinking":
			text := strings.TrimSpace(blk.Text)
			if blk.Type == "thinking" {
				// Claude Code keeps some of Claude's messages to the user as
				// "narration" blocks; its own thinking is never shown.
				kind := signatureKind(blk.Signature)
				if kind == "" && blk.Signature != "" {
					logUnknownSignature.Do(func() {
						log.Printf("[follow] A thinking block's signature names neither narration nor thinking: Claude Code may have changed its format, and Claude's messages to you may be missing from the window")
					})
				}
				if kind != "narration" {
					continue
				}
				text = strings.TrimSpace(blk.Thinking)
			}
			if text == "" {
				continue
			}
			// Text blocks of one message belong together; a new message
			// replaces the narration.
			if t.latest != nil && e.Message.ID != "" && b.lastTextMsg == e.Message.ID {
				t.latest.text = capString(t.latest.text+"\n\n"+text, followTextMax)
			} else {
				if t.latest != nil {
					t.trail = append(t.trail, t.latest)
					if len(t.trail) > followTrailMax {
						t.trail = t.trail[len(t.trail)-followTrailMax:]
					}
				}
				t.latest = &narrated{at: e.Timestamp, text: capString(text, followTextMax)}
			}
			b.lastTextMsg = e.Message.ID
		case "tool_use":
			if blk.ID == "" {
				continue
			}
			in := parseToolInput(blk.Input)
			if t.latest != nil {
				t.latest.tools = addTool(t.latest.tools, blk.Name)
			} else {
				t.preTools = addTool(t.preTools, blk.Name)
			}
			c := &toolCall{name: blk.Name, at: e.Timestamp, turn: t}
			c.activity, c.summary = describeTool(blk.Name, in)
			if c.activity != "" {
				t.doing = c.activity
			}
			if blk.Name == "AskUserQuestion" {
				c.question = questionFromInput(in)
			}
			b.calls[blk.ID] = c
			if in.RunInBackground {
				// Claude carries on with this later, when it reports back.
				b.background[blk.ID] = &bgTask{what: c.activity, since: e.Timestamp}
			}
			if id := in.TaskID + in.ShellID; id != "" && (blk.Name == "TaskStop" || blk.Name == "KillShell" || blk.Name == "KillBash") {
				b.endBackground(id)
			}
		}
	}
	switch e.Message.StopReason {
	case "":
		// Streaming chunk without a verdict: keep the current state.
	case "tool_use", "pause_turn":
		b.inTurn = true
	default: // end_turn, stop_sequence, max_tokens, refusal
		b.inTurn = false
		end := e.Timestamp
		t.end = &end
	}
}

func (b *followBuilder) onUser(e *followEntry) {
	blocks := parseContentBlocks(e.Message.Content)
	results := false
	for _, blk := range blocks {
		if blk.Type == "tool_result" {
			results = true
			if t := b.background[blk.ToolUseID]; t != nil && t.taskID == "" {
				if m := bgLaunchRe.FindStringSubmatch(normalizeResultContent(blk.Content)); m != nil {
					t.taskID = m[1]
				}
			}
			if c := b.calls[blk.ToolUseID]; c != nil {
				delete(b.calls, blk.ToolUseID)
				b.touch(c.turn, e.Timestamp)
			}
		}
	}
	if results || len(e.ToolUseResult) > 0 || e.IsMeta || e.IsCompactSummary {
		return
	}
	text := promptText(blocks)
	switch promptKind(e, text) {
	case "interrupt":
		if t := b.current(); t != nil {
			t.interrupted = true
			end := e.Timestamp
			t.end = &end
			b.touch(t, e.Timestamp)
		}
		b.inTurn = false
		b.calls = map[string]*toolCall{}
	case "human":
		b.onPrompt(text, e.Timestamp)
	case "auto":
		// A background task finished or a peer wrote in: Claude resumes the
		// current turn rather than starting one the user asked for.
		for _, m := range bgNoticeRe.FindAllStringSubmatch(text, -1) {
			b.endBackground(m[1])
		}
		if t := b.current(); t != nil {
			b.touch(t, e.Timestamp)
		}
		b.inTurn = true
	}
}

// onAttachment ends the background tasks whose notice Claude Code absorbed
// into the running turn: a task that finishes while Claude is still at work
// reports there, not in a user line, and would otherwise keep a finished
// turn "in the background".
func (b *followBuilder) onAttachment(e *followEntry) {
	a := e.Attachment
	if a == nil || a.Type != "queued_command" || a.CommandMode != "task-notification" {
		return
	}
	for _, blk := range parseContentBlocks(a.Prompt) {
		for _, m := range bgNoticeRe.FindAllStringSubmatch(blk.Text, -1) {
			b.endBackground(m[1])
		}
	}
}

func (b *followBuilder) onPrompt(text string, ts time.Time) {
	b.lastPrompt = ts
	if isRequest(text) {
		b.task = capString(text, followPromptMax)
	}
	cur := b.current()
	if cur != nil && b.inTurn && !cur.interrupted && ts.Sub(b.lastActivity) < followFollowUpGap {
		b.touch(cur, ts) // typed mid-turn: a follow-up, same turn
		return
	}
	if cur != nil && cur.local && !cur.active {
		b.turns = b.turns[:len(b.turns)-1] // e.g. /model: nothing to show
	}
	t := &followTurn{prompt: capString(text, followPromptMax), start: ts, lastActivity: ts, local: strings.HasPrefix(text, "/")}
	b.turns = append(b.turns, t)
	if len(b.turns) > 2 {
		b.turns = b.turns[len(b.turns)-2:]
	}
	b.calls = map[string]*toolCall{} // calls of earlier turns will never answer now
	b.touch(t, ts)
	b.inTurn = !t.local
}

// snapshot copies the builder into a Follow without live state (see
// deriveState).
func (b *followBuilder) snapshot() *Follow {
	f := &Follow{CWD: b.cwd, LastActivity: b.lastActivity}
	if !b.lastPrompt.IsZero() {
		lp := b.lastPrompt
		f.LastPromptAt = &lp
	}
	// A trailing slash command Claude never answered does not hide the turn
	// before it.
	t := b.current()
	if t != nil && t.local && !t.active && len(b.turns) > 1 {
		t = b.turns[len(b.turns)-2]
	}
	if t == nil {
		return f
	}
	start := t.start
	f.Prompt, f.TurnStart, f.Interrupted = t.prompt, &start, t.interrupted
	f.Task = b.task
	if f.Task == "" {
		f.Task = t.prompt
	}
	if t.end != nil {
		end := *t.end
		f.TurnEnd = &end
	}
	if t.latest != nil {
		n := *t.latest
		n.tools = append([]toolCount(nil), n.tools...)
		f.latest = &n
	}
	for _, n := range t.trail {
		c := *n
		c.tools = append([]toolCount(nil), c.tools...)
		f.trail = append(f.trail, c)
	}
	f.preTools = append([]toolCount(nil), t.preTools...)
	f.doing = t.doing
	for _, c := range b.calls {
		if c.turn == t {
			f.pending = append(f.pending, pendingCall{name: c.name, activity: c.activity, summary: c.summary, since: c.at, question: c.question})
		}
	}
	sort.Slice(f.pending, func(i, j int) bool { return f.pending[i].since.Before(f.pending[j].since) })
	for _, bt := range b.background {
		f.bg = append(f.bg, *bt)
	}
	sort.Slice(f.bg, func(i, j int) bool { return f.bg[i].since.Before(f.bg[j].since) })
	return f
}

// deriveState sets State, Activity, NeedsYou and the headline from the
// transcript and the live process status (pid nil when the process is gone).
func (f *Follow) deriveState(pid *PIDInfo, now time.Time) {
	f.Running = pid != nil
	if pid != nil {
		f.Entrypoint = pid.Entrypoint
	}
	f.Trail = []TrailItem{}
	if f.TurnStart == nil {
		f.State = FollowEmpty
		// Busy for a while, yet no prompt softgaze can read: the
		// transcript is not what the parser expects. (A shell command or a
		// dialog before the first prompt has no prompt either: not this.)
		if pid != nil && pid.Status == "busy" && pid.StatusUpdatedAt > 0 &&
			now.Sub(time.UnixMilli(pid.StatusUpdatedAt)) > followUnsureGrace {
			f.State = FollowUnsure
			f.Unsure = "Claude Code says it is busy, but softgaze finds no prompt in the transcript."
		}
		return
	}
	var p *pendingCall
	if n := len(f.pending); n > 0 {
		p = &f.pending[n-1]
	}
	open := f.TurnEnd == nil && !f.Interrupted
	switch {
	case pid == nil:
		f.State = FollowEnded
	case p != nil && p.question != nil:
		f.State = FollowNeedsYou
		q := *p.question
		f.NeedsYou = &q
	case p != nil && p.name == "ExitPlanMode":
		f.State = FollowNeedsYou
		f.NeedsYou = &NeedsYou{Kind: "plan", Text: "Approve or reject the plan"}
	case pid.Status == "waiting":
		f.State = FollowNeedsYou
		if p != nil {
			f.NeedsYou = &NeedsYou{Kind: "approval", Text: p.summary}
		} else {
			f.NeedsYou = &NeedsYou{Kind: "input", Text: waitingText(pid.WaitingFor)}
		}
	case pid.Status == "busy" || pid.Status == "shell":
		f.State = FollowWorking
	case pid.Status == "":
		f.State = FollowUnsure
		f.Unsure = "Claude Code does not report what this session is doing."
	case !knownStatus[pid.Status]:
		f.State = FollowUnsure
		f.Unsure = "Claude Code reports a status softgaze does not know (" + strconv.Quote(pid.Status) + ")."
		logUnknownStatus(pid.Status)
	default: // idle
		f.State = FollowFinished
		// Background work started in this turn by this Claude Code process
		// (not one it was resumed from) and not reported back: Claude will
		// carry on. Work from an earlier turn is not what Claude waits on
		// (the user prompted since and got an answer), and some of it never
		// reports at all (a dev server).
		var live []bgTask
		for _, t := range f.bg {
			if now.Sub(t.since) <= followBgMax && !t.since.Before(*f.TurnStart) && (pid.StartedAt == 0 || t.since.UnixMilli() >= pid.StartedAt) {
				live = append(live, t)
			}
		}
		if n := len(live); n > 0 && !f.Interrupted {
			f.State = FollowBackground
			f.Background = &Background{What: live[n-1].what, Since: live[n-1].since, Count: n}
		}
	}
	if f.State == FollowWorking || f.State == FollowNeedsYou {
		if p != nil {
			f.Activity = p.activity
			since := p.since
			f.ActivitySince = &since
		} else if f.State == FollowWorking {
			f.Activity = "thinking"
		}
	}
	if f.State == FollowWorking {
		f.Doing = f.doing
	}

	// Headline: the latest narration while Claude works, the end of its final
	// message once it stops. Unsure, the transcript decides which.
	f.mid = f.State == FollowWorking || f.State == FollowNeedsYou || (f.State == FollowUnsure && open)
	if last := f.latest; last != nil {
		at := last.at
		f.HeadlineAt = &at
		if f.mid {
			f.Headline = headText(last.text, followHeadlineMax)
		} else {
			f.Headline = tailText(last.text, followFinalMax)
		}
	}
	f.Trail = f.trailText()
	if f.latest != nil {
		f.Tools = toolsText(f.latest.tools)
	} else {
		f.Tools = toolsText(f.preTools)
	}
	if (f.State == FollowFinished || f.State == FollowEnded) && !f.Interrupted && f.latest != nil {
		if q := trailingQuestion(f.latest.text); q != "" {
			f.NeedsYou = &NeedsYou{Kind: "reply", Text: q}
		}
	}
}

// trailText is the context before the headline, oldest first. While Claude
// works: how its earlier messages of the turn began. Once it stops (the
// headline being the end of its final message): the rest of that message,
// and before it the end of the message before, when room is left.
func (f *Follow) trailText() []TrailItem {
	out := []TrailItem{}
	if f.latest == nil {
		return out
	}
	working := f.mid
	var own string // the final message without its last paragraph
	if !working {
		// What the headline (the end of the final message) leaves out.
		if _, rest := tailLines(f.latest.text, followFinalMax); rest != "" {
			own = tailText(rest, followTrailItem)
		}
	}
	room := followTrailMax
	if own != "" {
		room--
	}
	from := len(f.trail) - room
	if from < 0 {
		from = 0
	}
	tools := func(list []toolCount) {
		if len(list) > 0 {
			out = append(out, TrailItem{Kind: "tools", Text: toolsText(list)})
		}
	}
	if from == 0 {
		tools(f.preTools)
	}
	for _, n := range f.trail[from:] {
		text := headText(n.text, followTrailItem)
		if !working {
			text = tailText(n.text, followTrailItem)
		}
		out = append(out, TrailItem{Kind: "text", Text: text})
		tools(n.tools)
	}
	if own != "" {
		out = append(out, TrailItem{Kind: "text", Text: own})
	}
	return out
}

// toolInput holds the input fields a tool description needs.
type toolInput struct {
	FilePath        string `json:"file_path"`
	NotebookPath    string `json:"notebook_path"`
	Command         string `json:"command"`
	Description     string `json:"description"`
	Pattern         string `json:"pattern"`
	URL             string `json:"url"`
	Query           string `json:"query"`
	Skill           string `json:"skill"`
	RunInBackground bool   `json:"run_in_background"`
	TaskID          string `json:"task_id"`
	ShellID         string `json:"shell_id"`
	Questions       []struct {
		Question string `json:"question"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"questions"`
}

func parseToolInput(raw json.RawMessage) toolInput {
	var in toolInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &in)
	}
	return in
}

// describeTool returns what a pending call looks like in plain words
// ("running go test ./...") and what it would do, for an approval prompt.
func describeTool(name string, in toolInput) (activity, summary string) {
	path := in.FilePath
	if path == "" {
		path = in.NotebookPath
	}
	base := filepath.Base(path)
	switch name {
	case "Edit", "MultiEdit", "NotebookEdit":
		return "editing " + base, "edit " + path
	case "Write":
		return "writing " + base, "write " + path
	case "Read":
		return "reading " + base, "read " + path
	case "Bash", "PowerShell":
		// Claude describes each command in a few words ("Run the test
		// suite"): easier to follow than the command itself, which stays the
		// summary an approval prompt shows.
		cmd := firstLine(in.Command)
		if d := strings.TrimSpace(in.Description); d != "" {
			return capString(d, 120), cmd
		}
		return "running " + capString(cmd, 120), cmd
	case "Grep", "Glob":
		return "searching the code", name + " " + in.Pattern
	case "Agent", "Task":
		d := in.Description
		if d == "" {
			d = "a subagent"
		}
		return "subagent: " + d, d
	case "WebFetch":
		return "reading " + in.URL, in.URL
	case "WebSearch":
		return "searching the web", in.Query
	case "AskUserQuestion":
		return "asking you a question", ""
	case "ExitPlanMode":
		return "waiting for plan approval", "approve the plan"
	case "TodoWrite":
		return "updating its todo list", ""
	case "Skill":
		return "running skill " + in.Skill, in.Skill
	}
	if strings.HasPrefix(name, "mcp__") {
		if parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2); len(parts) == 2 {
			return "using " + parts[0] + " (" + parts[1] + ")", name
		}
	}
	return "using " + name, name
}

func questionFromInput(in toolInput) *NeedsYou {
	if len(in.Questions) == 0 {
		return &NeedsYou{Kind: "question", Text: "Claude asked you a question"}
	}
	q := in.Questions[0]
	n := &NeedsYou{Kind: "question", Text: q.Question}
	n.Text, n.Context = splitQuestion(q.Question)
	for _, o := range q.Options {
		n.Options = append(n.Options, o.Label)
	}
	if len(in.Questions) > 1 {
		n.Text += " (+" + strconv.Itoa(len(in.Questions)-1) + " more)"
	}
	return n
}

func waitingText(w string) string {
	switch w {
	case "":
		return "Claude Code is waiting for you"
	case "dialog open":
		return "A dialog is open in Claude Code"
	case "input needed":
		return "Claude Code needs your input"
	case "sandbox request":
		return "Approve or deny a sandbox request"
	case "worker request":
		return "A worker is asking for approval"
	}
	return "Claude Code is waiting: " + w
}

var (
	reminderRe   = regexp.MustCompile(`(?s)<(system-reminder|ide_opened_file|ide_selection|ide_diagnostics)>.*?</(system-reminder|ide_opened_file|ide_selection|ide_diagnostics)>`)
	cmdNameRe    = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	cmdArgsRe    = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	localCmdRe   = regexp.MustCompile(`^\s*<(local-command-|command-message>|bash-stdout|bash-stderr)`)
	notifyTextRe = regexp.MustCompile(`^\s*<(task-notification|peer-message|scheduled-task)`)
)

// promptText renders a user message as the prompt the person typed: IDE
// context and reminders removed, slash commands shown as "/name args".
func promptText(blocks []contentBlock) string {
	var parts []string
	images := 0
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			parts = append(parts, blk.Text)
		case "image":
			images++
		}
	}
	text := strings.Join(parts, "\n")
	if m := cmdNameRe.FindStringSubmatch(text); m != nil {
		name := strings.TrimSpace(m[1])
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		if a := cmdArgsRe.FindStringSubmatch(text); a != nil && strings.TrimSpace(a[1]) != "" {
			name += " " + strings.TrimSpace(a[1])
		}
		return name
	}
	text = strings.TrimSpace(reminderRe.ReplaceAllString(text, ""))
	if images > 0 {
		label := "[image]"
		if images > 1 {
			label = "[" + strconv.Itoa(images) + " images]"
		}
		text = strings.TrimSpace(text + " " + label)
	}
	return text
}

// promptKind classifies a user line that carries no tool result: a prompt the
// person typed ("human"), an automatic continuation ("auto"), an interrupt
// marker, or noise ("none").
func promptKind(e *followEntry, text string) string {
	if strings.HasPrefix(text, "[Request interrupted") {
		return "interrupt"
	}
	if text == "" {
		return "none"
	}
	if e.Origin != nil && e.Origin.Kind != "" {
		if e.Origin.Kind == "human" {
			return "human"
		}
		return "auto"
	}
	if e.TurnOrigin != "" {
		if e.TurnOrigin == "human" {
			return "human"
		}
		return "auto"
	}
	// Older transcripts carry no origin: fall back to the text.
	if localCmdRe.MatchString(text) {
		return "none"
	}
	if notifyTextRe.MatchString(text) {
		return "auto"
	}
	return "human"
}

var (
	mdLinkRe = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdLeadRe = regexp.MustCompile(`(?m)^\s*(?:[-*+>•]|#{1,6}|\d+[.)])\s+`)
	spacesRe = regexp.MustCompile(`\s+`)
	// A markdown list item, and a table's |---|---| rule line.
	listItemRe  = regexp.MustCompile(`^(?:[-*+•]|\d+[.)])\s+`)
	tableRuleRe = regexp.MustCompile(`^\|?[\s:|-]+\|[\s:|-]*$`)
	sentenceRe  = regexp.MustCompile(`[.!?:](?:\s|$)`)
)

// plain flattens a bit of markdown into one line of text for a small window.
func plain(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdLeadRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	return strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))
}

// msgLines is a message as the window shows it: one line per paragraph line
// with markdown flattened, and no code blocks or table rules. Where the
// message leaves a blank line (or a code block) there is one empty line.
// List items keep their marker ("• " for a bullet, "2. " for a step) and
// their nesting, two spaces per level below the first.
func msgLines(text string) []string {
	var out []string
	var indents []int // the open list levels, by their indentation
	fence, gap := false, false
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			fence = !fence
			gap = len(out) > 0
			continue
		}
		if fence || tableRuleRe.MatchString(t) {
			continue
		}
		if t == "" {
			gap = len(out) > 0
			continue
		}
		mark := listItemRe.FindString(t)
		if t = plain(t); t == "" {
			continue
		}
		if mark == "" {
			indents = indents[:0]
		} else {
			w := len(ln) - len(strings.TrimLeft(ln, " \t"))
			for len(indents) > 0 && w < indents[len(indents)-1] {
				indents = indents[:len(indents)-1]
			}
			if len(indents) == 0 || w > indents[len(indents)-1] {
				indents = append(indents, w)
			}
			m := strings.TrimSpace(mark)
			if m[0] >= '0' && m[0] <= '9' {
				m = strings.TrimRight(m, ".)") + "."
			} else {
				m = "•"
			}
			t = strings.Repeat("  ", min(len(indents)-1, 3)) + m + " " + t
		}
		if gap {
			out = append(out, "")
			gap = false
		}
		out = append(out, t)
	}
	return out
}

// headText returns the start of text, at most n bytes: whole lines while
// they fit (" …" marks the rest), or the first line cut at a sentence end
// when one falls in its second half, else at a word.
func headText(text string, n int) string {
	lines := msgLines(text)
	if len(lines) == 0 {
		return ""
	}
	if len(lines[0]) > n {
		return headLine(lines[0], n)
	}
	out, size := lines[:1], len(lines[0])
	for _, l := range lines[1:] {
		if size+1+len(l) > n {
			return strings.TrimRight(strings.Join(out, "\n"), "\n") + " …"
		}
		out, size = append(out, l), size+1+len(l)
	}
	return strings.Join(out, "\n")
}

func headLine(text string, n int) string {
	cut := -1
	for _, m := range sentenceRe.FindAllStringIndex(text[:n], -1) {
		cut = m[0] + 1
	}
	if cut > n/2 {
		return text[:cut]
	}
	return cutAtWord(text, n)
}

// tailText returns the end of text, at most n bytes: the last whole lines
// while they fit ("…" marks what came before), or the last line starting at
// a sentence when cut: the part of a final message that says what is next.
func tailText(text string, n int) string {
	t, _ := tailLines(text, n)
	return t
}

// tailLines is tailText plus what it left out before, as message lines.
func tailLines(text string, n int) (tail, rest string) {
	lines := msgLines(text)
	if len(lines) == 0 {
		return "", ""
	}
	last := lines[len(lines)-1]
	if len(last) > n {
		return tailLine(last, n), strings.Join(lines[:len(lines)-1], "\n")
	}
	from, size := len(lines)-1, len(last)
	for from > 0 && size+1+len(lines[from-1]) <= n {
		from--
		size += 1 + len(lines[from])
	}
	for lines[from] == "" {
		from++
	}
	tail = strings.Join(lines[from:], "\n")
	if from > 0 {
		tail = "…" + tail
	}
	return tail, strings.Join(lines[:from], "\n")
}

func tailLine(text string, n int) string {
	start := len(text) - n
	for start < len(text) && !isRuneStart(text[start]) {
		start++
	}
	tail := text[start:]
	// The cut lands mid-sentence: start at the next sentence if one follows.
	if m := sentenceRe.FindStringIndex(tail); m != nil && strings.TrimSpace(tail[m[1]:]) != "" {
		tail = tail[m[1]:]
	}
	return "…" + strings.TrimSpace(tail)
}

// cutAtWord truncates text to at most n bytes at a word boundary and marks
// the cut with an ellipsis.
func cutAtWord(text string, n int) string {
	t := capString(text, n)
	if i := strings.LastIndexByte(t, ' '); i > n/2 {
		t = t[:i]
	}
	return strings.TrimRight(t, " ,;:") + "…"
}

// trailingQuestion returns the question Claude ended its message with, if any:
// the last sentence of the last paragraph, when it ends with "?".
func trailingQuestion(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasSuffix(text, "?") {
		return ""
	}
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		text = strings.TrimSpace(text[i+1:])
	}
	text = plain(text)
	start := 0
	for i := len(text) - 2; i > 0; i-- {
		if (text[i] == '.' || text[i] == '!' || text[i] == '?') && text[i+1] == ' ' {
			start = i + 2
			break
		}
	}
	return capString(strings.TrimSpace(text[start:]), 300)
}

// followQuestionLong: a question longer than this has its last sentence
// put first (see splitQuestion).
const followQuestionLong = 120

// splitQuestion splits a long question into its last sentence, the question
// itself, and the context before it. Short ones, and those whose last
// sentence is not a real question, come back whole.
func splitQuestion(text string) (question, context string) {
	text = strings.TrimSpace(text)
	if len(text) <= followQuestionLong || strings.Contains(text, "\n") {
		return text, ""
	}
	q := trailingQuestion(text)
	if len(q) < 12 || len(q) >= len(text) || !strings.HasSuffix(text, q) {
		return text, ""
	}
	return q, strings.TrimSpace(strings.TrimSuffix(text, q))
}

// isRequest reports whether a prompt states a task rather than answering
// Claude ("yes", "1", "go ahead") or running a slash command.
func isRequest(text string) bool {
	if strings.HasPrefix(text, "/") {
		return false
	}
	return len(strings.Fields(text)) >= 4
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " ..."
	}
	return s
}

// capString truncates s to at most n bytes on a rune boundary.
func capString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// logUnknownSignature notes, once, a thinking block whose signature names
// a kind softgaze does not know.
var logUnknownSignature sync.Once

// isNarration reports whether a thinking block's signature marks it as a
// message to the user rather than Claude's thinking.
func isNarration(signature string) bool { return signatureKind(signature) == "narration" }

// signatureKind is the kind of block a thinking block's signature names:
// Claude Code's signatures begin with a small header naming it
// ("narration" or "thinking"), so only that start is decoded. "" when it
// names neither.
func signatureKind(signature string) string {
	head := signature
	if len(head) > 64 {
		head = head[:64]
	}
	head = head[:len(head)/4*4]
	raw, err := base64.StdEncoding.DecodeString(head)
	switch {
	case err != nil:
		return ""
	case bytes.Contains(raw, []byte("narration")):
		return "narration"
	case bytes.Contains(raw, []byte("thinking")):
		return "thinking"
	}
	return ""
}
