// Softgaze: a small window that keeps you with your task while Claude
// works. Plain JS, no dependencies, styles included. Used by follow.html.
//
// Design rules, from the research on focus and interruptions:
// - Keep the goal in sight: your task stays on screen, so it stays active in
//   memory instead of decaying while you wait (memory for goals).
// - Explain the wait: what Claude says it is doing, and elapsed time (never a
//   countdown); occupied, explained waits feel shorter than empty ones.
// - Stay calm: no animation, no ticking seconds; motion is what pulls the eye
//   off the task in peripheral displays.
// - Signal once, at the breakpoint: when Claude stops or needs you, the
//   window changes to a steady state that holds until you are back.
// - One task: the window locks onto one session and never jumps to another
//   by itself. Switching is a deliberate act: click the project name, pick
//   from a list that loads once and does not move.
// - Permission to wait: while Claude works, the window says there is nothing
//   to do and that it will change when it is your turn. People enjoy a few
//   minutes with their own thoughts more than they expect, and the phone
//   often fills a gap that does not need filling.
// - Notice the focus, never score it (config wellbeing): how long you have
//   stayed with this session, as plain minutes. No records, no streaks, no
//   points, and never a judgment of slow replies.
(function () {
  'use strict';

  const CSS = `
.sg-root {
  --sg-bg: #1a1a1a; --sg-text: #d4c5b9; --sg-dim: #8a7f75; --sg-bright: #f0e6dc;
  --sg-work: #8fd0c8; --sg-done: #a8dca8; --sg-needs: #f0a080;
  position: relative; box-sizing: border-box; height: 100vh; overflow: hidden;
  /* A reading column of about 80 characters, centered in a wide window. */
  padding: 0.75em max(1em, calc((100% - 44em) / 2)) 0.7em;
  display: flex; flex-direction: column; gap: 0.4em;
  font-family: 'Segoe UI', system-ui, -apple-system, sans-serif; font-size: clamp(13px, 3.4vw, 18px);
  line-height: 1.4; color: var(--sg-text); background: var(--sg-bg);
  border-top: 3px solid var(--sg-work);
}
.sg-root * { box-sizing: border-box; }
/* Lines keep their full height; fit() drops whole lines that do not fit. */
.sg-root > * { flex-shrink: 0; }
.sg-root .sg-drop { display: none !important; }
.sg-root.sg-none { border-top-color: transparent; }
.sg-root.sg-needs_you { border-top: 6px solid var(--sg-needs); background: #2a1f18; }
.sg-root.sg-finished { border-top: 6px solid var(--sg-done); background: #1c261c; }
/* Evening (7 pm to 7 am): warmer colors and a little less contrast. */
.sg-root.sg-evening { --sg-bg: #1b1815; --sg-text: #c9b9a7; --sg-dim: #847868; --sg-bright: #e2d2be; }
.sg-root.sg-evening.sg-finished { background: #1d211a; }
.sg-root.sg-evening.sg-needs_you { background: #261c16; }
/* A change of state fades in, like a turn in a conversation; nothing else moves. */
.sg-root { transition: background-color 0.6s ease, border-color 0.6s ease; }
.sg-root.sg-enter > * { animation: sg-fade 0.5s ease-out; }
@keyframes sg-fade { from { opacity: 0; } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .sg-root { transition: none; } .sg-root.sg-enter > * { animation: none; } }
/* A long run without a pause: a break would fit here (shown once per run). */
.sg-break { font-size: 0.85em; color: var(--sg-text); }
/* The minutes of the work period, in the switch list. */
.sg-today { color: var(--sg-dim); font-size: 0.8em; padding: 0 0.4em; }
.sg-root.sg-ended { border-top-color: #5a5048; }
/* Can't tell: no state color, since softgaze cannot back one. */
.sg-root.sg-unsure { border-top: 3px dashed var(--sg-dim); }
.sg-unsure .sg-state, .sg-unsure .sg-cta { color: var(--sg-text); }
.sg-why { font-size: 0.85em; color: var(--sg-text); }
.sg-top { display: flex; align-items: baseline; gap: 0.6em; font-size: 0.78em; white-space: nowrap; }
.sg-root .sg-project { font-weight: 700; color: var(--sg-bright); overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.sg-root button { font: inherit; color: inherit; background: none; border: 0; margin: 0; padding: 0; cursor: pointer; }
.sg-root button:focus-visible { outline: 2px solid var(--sg-work); outline-offset: 2px; border-radius: 3px; }
.sg-switch { display: inline-flex; align-items: baseline; gap: 0.3em; max-width: 100%; text-align: left; }
.sg-name { overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.sg-chev { color: var(--sg-dim); flex: 0 0 auto; font-weight: 400; }
.sg-switch:hover .sg-name { text-decoration: underline; }
.sg-switch:hover .sg-chev { color: var(--sg-bright); }
.sg-root .sg-editor { color: var(--sg-dim); font-weight: 400; flex: 0 0 auto; }
.sg-root .sg-editor:hover { color: var(--sg-bright); text-decoration: underline; }
.sg-root .sg-link { color: var(--sg-work); text-decoration: underline; }
.sg-root .sg-cancel { margin-left: auto; color: var(--sg-dim); font-weight: 400; }
.sg-root.sg-picking { border-top-color: #5a5048; }
.sg-root > .sg-list { flex: 1 1 auto; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 2px; }
.sg-root .sg-row { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.15em 0.6em; width: 100%; padding: 0.35em 0.4em; border-radius: 4px;
  font-size: 0.85em; text-align: left; white-space: nowrap; color: var(--sg-text); }
.sg-root .sg-row:hover { background: #262626; }
/* The list scrolls, so it clips what is drawn outside it: the focus ring
   goes just inside each row instead. */
.sg-root .sg-list button:focus-visible { outline-offset: -2px; }
/* Claude's last words in each session, under its row. */
.sg-row-last { flex: 1 0 100%; padding-left: 1.6em; box-sizing: border-box; color: var(--sg-dim); font-size: 0.92em;
  overflow: hidden; text-overflow: ellipsis; }
.sg-row-key { flex: 0 0 1em; color: var(--sg-dim); font-weight: 400; font-variant-numeric: tabular-nums; }
.sg-row-project { font-weight: 700; color: var(--sg-bright); overflow: hidden; text-overflow: ellipsis; max-width: 40%; flex: 0 0 auto; }
.sg-row-task { color: var(--sg-dim); overflow: hidden; text-overflow: ellipsis; min-width: 0; flex: 1 1 auto; }
.sg-row-state { flex: 0 0 auto; font-weight: 700; color: var(--sg-work); }
.sg-row-state.sg-s-needs_you { color: var(--sg-needs); }
.sg-row-state.sg-s-finished { color: var(--sg-done); }
.sg-row-state.sg-s-unsure { color: var(--sg-dim); }
.sg-current .sg-row-state { color: var(--sg-dim); font-weight: 400; }
.sg-note { color: var(--sg-dim); font-size: 0.8em; padding: 0.35em 0.4em; }
.sg-root .sg-more { font-size: 0.8em; padding: 0.35em 0.4em; text-align: left; color: var(--sg-dim); }
.sg-root .sg-more:hover { color: var(--sg-bright); }
.sg-older .sg-row-project { color: var(--sg-text); font-weight: 400; }
.sg-state { margin-left: auto; font-weight: 700; flex: 0 0 auto; color: var(--sg-work); }
.sg-newer { margin-left: 0.5em; color: var(--sg-dim); font-weight: 400; }
.sg-needs_you .sg-state { color: var(--sg-needs); }
.sg-finished .sg-state { color: var(--sg-done); }
.sg-ended .sg-state { color: var(--sg-dim); }
.sg-task { font-size: 0.85em; color: var(--sg-text); overflow-wrap: anywhere;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.sg-task b { color: var(--sg-dim); font-weight: 400; }
.sg-headline { white-space: pre-line; font-size: 1.05em; line-height: 1.35; color: var(--sg-bright); overflow-wrap: anywhere;
  display: -webkit-box; -webkit-box-orient: vertical; overflow: hidden; }
.sg-quiet { color: var(--sg-dim); }
/* Claude's message: a gap where it left a blank line, list items with a
   hanging indent (a wrapped line starts under the text, not the bullet). */
.sg-gap { height: 0.6em; }
.sg-li { position: relative; padding-left: 1.4em; }
.sg-li + .sg-li { margin-top: 0.25em; }
.sg-mk { position: absolute; left: 0; width: 1.1em; text-align: right; color: var(--sg-dim); font-variant-numeric: tabular-nums; }
/* Your turn, and Claude's final message is longer than the window: it scrolls. */
.sg-root .sg-headline.sg-scroll { display: block; flex: 1 1 0; min-height: 3em; overflow-y: auto; padding-right: 0.4em; }
/* Tools by name only: after each earlier message, and since the latest one. */
.sg-trail > .sg-tl, .sg-tools { font-size: 0.85em; opacity: 0.75; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; display: block; }
.sg-tools { font-size: 0.8em; color: var(--sg-dim); }
.sg-tl::before, .sg-tools::before { content: '\\2192  '; }
/* What Claude said before the headline, in every state. */
.sg-trail { display: flex; flex-direction: column; gap: 0.3em; font-size: 0.8em; color: var(--sg-dim);
  border-left: 2px solid #3a3a3a; padding-left: 0.6em; }
.sg-trail > div { white-space: pre-line; overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.sg-qctx { font-size: 0.8em; color: var(--sg-dim); overflow-wrap: anywhere;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
.sg-needs_you .sg-headline { font-weight: 600; }
.sg-options { font-size: 0.8em; color: var(--sg-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.sg-cta { font-size: 0.9em; font-weight: 700; margin-top: auto; }
.sg-needs_you .sg-cta { color: var(--sg-needs); }
.sg-finished .sg-cta { color: var(--sg-done); }
.sg-ended .sg-cta, .sg-hint { color: var(--sg-dim); font-weight: 400; }
/* Your turn: the whole window is the way back to VS Code. */
.sg-root.sg-go { cursor: pointer; }
.sg-go:hover .sg-foot-main::after { content: '  \\2192  VS Code'; font-weight: 400; opacity: 0.75; }
/* The bottom line: what to do (or not) on the left, your focus on the right. */
.sg-foot { display: flex; align-items: baseline; flex-wrap: wrap; gap: 0.2em 1em; }
.sg-foot-main { min-width: 0; }
/* The keys, out of sight until you ask for them with ?. */
.sg-keys { display: none; color: var(--sg-dim); font-weight: 400; font-size: 0.8em; white-space: nowrap; }
.sg-show-keys .sg-keys { display: inline; }
.sg-focus { margin-left: auto; flex: 0 0 auto; white-space: nowrap; font-size: 0.85em; font-weight: 400;
  color: var(--sg-dim); font-variant-numeric: tabular-nums; letter-spacing: 0.01em; }
.sg-focus b { color: var(--sg-bright); font-weight: 600; }
.sg-focus.sg-paused b { color: var(--sg-text); font-weight: 400; }
.sg-empty { margin: auto 0; text-align: center; color: var(--sg-dim); font-size: 0.9em; }
.sg-empty strong { display: block; color: var(--sg-bright); font-size: 1.05em; margin-bottom: 0.2em; }
.sg-offline { color: var(--sg-dim); font-size: 0.75em; }
@media (max-height: 140px) { .sg-task, .sg-options, .sg-hint { display: none; } .sg-headline { -webkit-line-clamp: 2; } }
`;

  const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
  const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ESC[c]);
  // Claude's message as blocks: its lines, a gap for each empty line, and
  // list items ("• " or "2. ", two leading spaces per nesting level) with
  // their marker set apart.
  function msgHTML(text) {
    return text.split('\n').map(l => {
      if (!l.trim()) return '<div class="sg-gap"></div>';
      const m = /^( *)(•|\d+\.) (.*)$/.exec(l);
      if (!m) return `<div>${esc(l)}</div>`;
      const lvl = Math.min(3, m[1].length >> 1);
      return `<div class="sg-li"${lvl ? ` style="margin-left:${lvl * 1.4}em"` : ''}><span class="sg-mk">${esc(m[2])}</span>${esc(m[3])}</div>`;
    }).join('');
  }

  const ms = iso => { const t = iso ? Date.parse(iso) : NaN; return isNaN(t) ? 0 : t; };
  // How long Claude's latest message stands alone before the tools used
  // since then are shown under it.
  const TOOLS_AFTER = 30000;
  // How long "Welcome back" stays after you return to the session.
  const WELCOME_FOR = 60000;
  // How long Claude's message stays on screen while it works before a newer
  // one takes its place: time to read it at an unhurried pace, never more
  // than READ_MAX, so the window does not fall far behind.
  const READ_BASE = 3000, READ_PER_WORD = 250, READ_MAX = 45000;
  const readFor = text => Math.min(READ_MAX, READ_BASE + READ_PER_WORD * String(text || '').split(/\s+/).filter(Boolean).length);
  // What to show for f, given the message on screen (onScreen: its session,
  // turn, time, text and trail, and when it appeared). While Claude works and
  // that message has not had its reading time, a newer one waits: the frame
  // keeps the message on screen (held), and until says when to look again.
  // Anything else (a new turn, Claude stops or needs you) shows at once.
  function hold(onScreen, f, now) {
    if (!f || f.state !== 'working' || !f.headline) return { frame: f, onScreen: null, until: 0 };
    const same = onScreen && onScreen.session === f.session_id && onScreen.turn === f.turn_start;
    if (same && onScreen.at === f.headline_at) {
      // The same message, perhaps grown: show it, its reading time runs on.
      return { frame: f, onScreen: { ...onScreen, text: f.headline, trail: f.trail }, until: 0 };
    }
    const until = same ? onScreen.since + readFor(onScreen.text) : 0;
    if (until > now) {
      const frame = { ...f, headline: onScreen.text, headline_at: onScreen.at, trail: onScreen.trail, tools: '', held: true };
      return { frame, onScreen, until };
    }
    return { frame: f, onScreen: { session: f.session_id, turn: f.turn_start, at: f.headline_at, text: f.headline, trail: f.trail, since: now }, until: 0 };
  }
  const hm = m => m < 60 ? m + ' min' : Math.floor(m / 60) + ' h' + (m % 60 ? ' ' + (m % 60) + ' min' : '');
  const evening = () => { const h = new Date().getHours(); return h >= 19 || h < 7; };
  // Whole minutes only: enough to know it is alive, too slow to catch the eye.
  function mins(d) {
    const m = Math.floor(Math.max(0, d) / 60000);
    return m < 1 ? 'under a minute' : m === 1 ? '1 min' : m + ' min';
  }
  const liveMins = iso => `<span data-sg-since="${esc(iso)}">${mins(Date.now() - ms(iso))}</span>`;

  function injectCSS(doc) {
    if (doc.getElementById('sg-style')) return;
    const st = doc.createElement('style');
    st.id = 'sg-style';
    st.textContent = CSS;
    (doc.head || doc.documentElement).appendChild(st);
  }

  // What the window says: your task, what Claude is doing (in its own words)
  // and how long it has been at it; or, at the breakpoint, what it needs.
  function view(f) {
    if (!f) {
      return { state: 'none', title: 'Softgaze', html: '<div class="sg-empty"><strong>No Claude Code session is running</strong>Send a prompt: this window locks onto that session.</div>' };
    }
    if (f.state === 'empty') {
      return { state: 'none', title: 'Softgaze', html: `<div class="sg-empty"><strong>${esc(f.project)}</strong>Waiting for your first prompt.</div>` };
    }
    const n = f.needs_you;
    // A one-line message is quoted; a longer one is laid out as blocks.
    const said = t => t.includes('\n') ? msgHTML(t) : '&ldquo;' + esc(t) + '&rdquo;';
    let label, headline, cta = '', extra = '';
    switch (f.state) {
      case 'working':
        label = 'Working · ' + (f.turn_start ? liveMins(f.turn_start) : '');
        // A newer message waits until you have read this one: a faint dot says so.
        if (f.held) label += '<span class="sg-newer" title="A newer message from Claude is next" aria-label="A newer message is next">&#8226;</span>';
        // No narration yet this turn (Claude went straight to a tool): say
        // what it is doing, in the words it gave the tool; between tools, what
        // it last set out to do. "Starting on it" only before the first tool.
        const doing = f.activity && f.activity !== 'thinking' ? f.activity : f.doing;
        headline = f.headline ? msgHTML(f.headline)
          : `<span class="sg-quiet">${doing ? esc(doing) : 'Claude is starting on it.'}</span>`;
        cta = 'Nothing to do until this changes color.';
        break;
      case 'needs_you':
        label = 'Needs you';
        headline = n && n.kind === 'approval' ? 'Wants to run: ' + esc(n.text) : esc(n ? n.text : 'Claude Code is waiting for you');
        if (n && n.options && n.options.length) extra = `<div class="sg-options">${n.options.map(esc).join(' · ')}</div>`;
        if (n && n.context) extra += `<div class="sg-qctx">${esc(n.context)}</div>`;
        cta = 'Answer in Claude Code';
        break;
      case 'finished':
        label = f.interrupted ? 'Stopped' : 'Finished' +
          (f.turn_start && f.turn_end ? ' · ' + mins(ms(f.turn_end) - ms(f.turn_start)) : '');
        headline = f.interrupted ? 'You stopped this turn.' : f.headline ? said(f.headline) : 'Claude is done.';
        cta = n && n.kind === 'reply' ? 'Your turn: answer its question' : 'Your turn';
        break;
      case 'background': {
        // Claude ended its turn but runs something in the background and will
        // carry on when it reports: not your turn yet.
        const bg = f.background || {};
        label = 'In the background' + (bg.since ? ' · ' + liveMins(bg.since) : '');
        headline = f.headline ? said(f.headline) : esc(bg.what || 'Claude is waiting on background work.');
        cta = 'Nothing to do yet: Claude carries on when ' + (bg.count > 1 ? 'its background work finishes.' : 'it finishes.');
        break;
      }
      case 'unsure':
        // softgaze cannot read what the session is doing (Claude Code
        // changed what it writes): say so, rather than a color it cannot back.
        label = "Can't tell";
        headline = f.headline ? said(f.headline) : '';
        extra = `<div class="sg-why">${esc(f.unsure || 'softgaze cannot read this session.')}</div>`;
        cta = 'Check Claude Code to see where it is.';
        break;
      default: // ended
        label = 'Session closed';
        headline = f.headline ? said(f.headline) : '';
        cta = 'This session is closed. <button class="sg-link" data-sg-act="pick">Switch session</button>';
    }
    // What Claude said before the headline, kept in every state.
    const tr = f.trail || [];
    const item = t => typeof t === 'string' ? `<div>${esc(t)}</div>`
      : `<div${t.kind === 'tools' ? ' class="sg-tl"' : ''}>${esc(t.text.replace(/\n\s*\n/g, '\n'))}</div>`;
    const trail = tr.length ? `<div class="sg-trail">${tr.map(item).join('')}</div>` : '';
    // The tools used since the latest message, while Claude is still at it,
    // and only once that message is half a minute old: then it says Claude
    // is still working, rather than ticking along under its words.
    const quiet = !f.headline_at || Date.now() - ms(f.headline_at) > TOOLS_AFTER;
    const tools = f.tools && quiet && f.state !== 'finished' && f.state !== 'ended' ? `<div class="sg-tools">${esc(f.tools)}</div>` : '';
    const html = `
      <div class="sg-top"><button class="sg-project sg-switch" data-sg-act="pick"><span class="sg-name">${esc(f.project)}</span><span class="sg-chev" aria-hidden="true">&#9662;</span></button><button class="sg-editor" data-sg-act="editor">VS Code &#8599;</button><span class="sg-state">${label}</span></div>
      ${(f.task || f.prompt) ? `<div class="sg-task"><b>Your task:</b> ${esc(f.task || f.prompt)}</div>` : ''}
      ${trail}<div class="sg-headline">${headline}</div>${tools}${extra}
      ${f.at_it_since && GO[f.state] ? `<div class="sg-break" data-sg-break="${esc(f.at_it_since)}" data-sg-turn="${esc(f.turn_start || '')}">You've been at it since ${esc(when(f.at_it_since))}. A short break would fit here.</div>` : ''}
      <div class="sg-cta sg-foot"><span class="sg-foot-main${f.state === 'working' || f.state === 'background' ? ' sg-hint' : ''}">${cta}</span><span class="sg-keys">S switch · &#8592; &#8594; · F full screen · Enter VS Code · Ctrl+Alt+J both ways · ? hide</span>${focusSlot(f)}</div>`;
    return { state: f.state, title: (TITLES[f.state] || 'Softgaze') + ' · ' + f.project, html };
  }

  // Where the focus stretch goes; mount() fills it in and keeps it current,
  // so its minutes move without redrawing the view.
  function focusSlot(f) {
    const fo = f.focus;
    if (!fo || f.state === 'ended') return '';
    return `<span class="sg-focus" data-sg-focus="${esc(fo.since)}" data-sg-project="${esc(f.project)}"` +
      ` data-sg-away="${Math.max(0, Math.floor(+fo.away_ms || 0))}" data-sg-away-since="${esc(fo.away_since || '')}"></span>`;
  }
  const TITLES = { working: 'Working', needs_you: 'Needs you', finished: 'Your turn', background: 'In the background', ended: 'Closed', unsure: "Can't tell" };
  // States where it is your turn, or where only Claude Code can say:
  // clicking the window takes you to VS Code.
  const GO = { finished: true, needs_you: true, unsure: true };

  // "09:05" today, "Wed 14:10" before.
  function when(iso) {
    const d = new Date(iso);
    if (isNaN(d)) return '';
    const hm = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    return d.toDateString() === new Date().toDateString() ? hm : d.toLocaleDateString([], { weekday: 'short' }) + ' ' + hm;
  }

  // The switch list: the running sessions of the current work period (the
  // ones prompted since the user's last long break, see /api/follow/sessions),
  // the one followed now marked; the others one click away. It is a snapshot
  // taken when the list opens, so nothing in it moves.
  // "Today: 2 h 40 min with your sessions" ('' under a minute). Plain
  // minutes: no count of turns, nothing to beat.
  function todayText(m) {
    return m >= 1 ? `Today: ${hm(m)} with your sessions` : '';
  }
  // The end of a message: its last line, at most about 220 characters
  // (the switch list shows where each session stands).
  function lastWords(text) {
    const lines = String(text || '').split('\n').map(l => l.trim()).filter(Boolean);
    let t = lines.length ? lines[lines.length - 1] : '';
    if (t.length > 220) { t = t.slice(-220); const i = t.indexOf(' '); t = '…' + (i > 0 ? t.slice(i + 1) : t); }
    return t;
  }
  function pickerHTML(p, followed) {
    const inPeriod = s => !p.current || p.current.has(s.session_id) || s.session_id === followed;
    const shown = p.all ? p.list : p.list.filter(inPeriod);
    const hidden = p.list.length - shown.length;
    const rows = shown.map((s, i) => {
      const cur = s.session_id === followed;
      return `<button class="sg-row${cur ? ' sg-current' : ''}${inPeriod(s) ? '' : ' sg-older'}" data-sg-act="choose" data-sg-id="${esc(s.session_id)}" title="${esc(s.cwd)}">` +
        `<span class="sg-row-key" aria-hidden="true">${i < 9 ? i + 1 : ''}</span><span class="sg-row-project">${esc(s.project)}</span><span class="sg-row-task">${esc(s.task || s.prompt)}</span>` +
        `<span class="sg-row-state sg-s-${esc(s.state)}">${cur ? 'Following' : esc(TITLES[s.state] || '')}</span>` +
        (s.headline ? `<span class="sg-row-last">${esc(lastWords(s.headline))}</span>` : '') + '</button>';
    }).join('');
    let extra = '';
    if (hidden) {
      const brk = p.breakMin >= 90 ? (p.breakMin / 60).toFixed(1).replace(/\.0$/, '') + ' h' : p.breakMin + ' min';
      const why = `Hidden: sessions you have not prompted since your last long break (a pause over ${brk}, ${p.learned ? 'learned from your history' : 'the default until there is more history'})`;
      extra = `<button class="sg-link sg-more" data-sg-act="more" title="${esc(why)}">${hidden} more, not prompted since ${esc(when(p.since))}</button>`;
    } else if (!shown.some(s => s.session_id !== followed)) {
      extra = '<div class="sg-note">No other session is running. A session shows up here after its first prompt.</div>';
    }
    const tt = todayText(p.minutes);
    const today = tt ? `<div class="sg-today">${tt}</div>` : '';
    return `<div class="sg-top"><span class="sg-project">Switch session</span>` +
      `<button class="sg-cancel" data-sg-act="cancel" title="Esc">Cancel</button></div>${today}<div class="sg-list">${rows}${extra}</div>`;
  }

  // Renders the live view into container (in any document) and keeps it
  // current from the SSE stream. The first session shown is locked: the view
  // never moves to another one unless the user picks it from the switch list.
  // Returns a handle with session() and destroy().
  function mount(container, opts) {
    opts = opts || {};
    const doc = container.ownerDocument;
    injectCSS(doc);
    container.className = 'sg-root sg-none';
    container.innerHTML = '<div class="sg-empty">Connecting...</div>';
    let es = null, stopped = false, retry = 2, offline = false, locked = opts.session || null, shown = '';

    // When the window is too small for everything, drop the least important
    // lines whole (the hint, the question's context, the answer options,
    // Claude's earlier messages oldest first, your task) instead of cutting
    // lines in half.
    // Claude's message gets all the room there is: it is shortened (to the
    // lines that fit, with an ellipsis) only once the other lines are gone.
    function fit() {
      container.querySelectorAll('.sg-drop').forEach(el => el.classList.remove('sg-drop'));
      const head = container.querySelector('.sg-headline');
      if (head) { head.style.webkitLineClamp = ''; head.classList.remove('sg-scroll'); }
      const fits = () => container.scrollHeight <= container.clientHeight;
      const drop = sel => { const el = container.querySelector(sel); if (el) el.classList.add('sg-drop'); };
      // Earlier messages go oldest first, down to the last two; then your
      // task, then those two: they tell you where Claude has got to.
      const trail = container.querySelector('.sg-trail');
      const items = trail ? [...trail.children] : [];
      const steps = [() => drop('.sg-hint'), () => drop('.sg-qctx'), () => drop('.sg-options')];
      items.slice(0, -2).forEach(el => steps.push(() => el.classList.add('sg-drop')));
      steps.push(() => drop('.sg-tools'), () => drop('.sg-task'));
      items.slice(-2).forEach(el => steps.push(() => el.classList.add('sg-drop')));
      if (trail) steps.push(() => trail.classList.add('sg-drop'));
      for (const step of steps) {
        if (fits()) return;
        step();
      }
      if (fits() || !head) return;
      // A final message is kept whole: it scrolls rather than being cut.
      if (container.classList.contains('sg-finished') || container.classList.contains('sg-ended')) { head.classList.add('sg-scroll'); return; }
      const lh = parseFloat((doc.defaultView || window).getComputedStyle(head).lineHeight) || 20;
      const over = container.scrollHeight - container.clientHeight;
      // Gaps between paragraphs and lists are not lines: trim until it fits.
      let lines = Math.max(2, Math.floor((head.clientHeight - over) / lh));
      head.style.webkitLineClamp = String(lines);
      while (!fits() && lines > 2) head.style.webkitLineClamp = String(--lines);
    }
    const win = doc.defaultView;
    // Timers run in the window being shown.
    const W = win || window;
    if (win) win.addEventListener('resize', fit);

    // The focus stretch: minutes with this session (time in front of other
    // windows does not count). While you are in another window it reads
    // "Paused"; after two minutes away the stretch is over and it goes. It
    // changes at most once a minute.
    const AWAY_MAX = 2 * 60000;
    // Away (paused, or the stretch ended) and back: "Welcome back" for a minute.
    let wasAway = false, backAt = 0;
    function paintFocus() {
      const el = container.querySelector('[data-sg-focus]');
      if (!el) return;
      const since = el.dataset.sgFocus;
      const now = Date.now(), awaySince = el.dataset.sgAwaySince ? ms(el.dataset.sgAwaySince) : 0;
      const awayNow = awaySince ? Math.max(0, now - awaySince) : 0;
      const over = awayNow > AWAY_MAX;
      const m = Math.floor(Math.max(0, now - ms(since) - (+el.dataset.sgAway || 0) - awayNow) / 60000);
      if (awaySince || over) wasAway = true;
      else if (wasAway) { wasAway = false; backAt = now; }
      const back = !awaySince && !over && backAt && now - backAt < WELCOME_FOR;
      const project = esc(el.dataset.sgProject || 'this session');
      const text = over ? '' : awaySince ? (m < 1 ? '' : `Paused at <b>${m}</b> min`)
        : back ? 'Welcome back' + (m >= 1 ? ` &middot; <b>${m}</b> min with ${project}` : '')
        : m < 1 ? '' : `<b>${m}</b> min with ${project}`;
      if (el.dataset.sgText !== text) {
        el.dataset.sgText = text;
        el.innerHTML = text;
        el.className = 'sg-focus' + (awaySince ? ' sg-paused' : '');
      }
    }

    // While the switch list is open, updates are kept (latest) and shown
    // when it closes.
    let picking = false, latest, choices = [], pick = null;
    // The break line shows on one turn per run of work (kept across reloads
    // when the browser allows it).
    let breaks = {};
    try { breaks = JSON.parse(W.localStorage.getItem('sg-breaks') || '{}') || {}; } catch (e) { breaks = {}; }
    function breakOnce(run, turn) {
      if (breaks[run] !== undefined && breaks[run] !== turn) return false;
      if (breaks[run] === undefined) {
        breaks[run] = turn;
        const keys = Object.keys(breaks);
        if (keys.length > 20) delete breaks[keys[0]];
        try { W.localStorage.setItem('sg-breaks', JSON.stringify(breaks)); } catch (e) { /* kept in memory */ }
      }
      return true;
    }
    // The message on screen while Claude works, and the timer that shows the
    // newer one once it has had its reading time (see hold).
    let onScreen = null, holdTimer = null;
    function render(f) {
      latest = f;
      if (picking) return;
      const h = hold(onScreen, f, Date.now());
      onScreen = h.onScreen;
      if (holdTimer) { W.clearTimeout(holdTimer); holdTimer = null; }
      if (h.until) holdTimer = W.setTimeout(() => { holdTimer = null; if (!picking && !stopped) render(latest); }, h.until - Date.now() + 50);
      const v = view(h.frame);
      const changed = state !== '' && state !== v.state;
      state = v.state;
      const html = v.html + (offline ? '<div class="sg-offline">Reconnecting...</div>' : '') +
        (notice ? `<div class="sg-offline">${esc(notice)}</div>` : '');
      // Touch the page only when something changed: no flicker, no motion.
      if (html !== shown) {
        container.className = 'sg-root sg-' + v.state + (GO[v.state] ? ' sg-go' : '') +
          (evening() ? ' sg-evening' : '') + (changed ? ' sg-enter' : '');
        container.innerHTML = html;
        shown = html;
        const brk = container.querySelector('[data-sg-break]');
        if (brk && !breakOnce(brk.dataset.sgBreak, brk.dataset.sgTurn)) brk.remove();
        paintFocus();
        fit(); // also forces layout now, so a fresh window draws it
      }
      doc.title = v.title;
    }

    let inflight = false, again = false, last = 0, timer = null;
    async function load() {
      if (stopped) return;
      if (inflight) { again = true; return; }
      inflight = true;
      const want = locked;
      try {
        const r = await fetch('/api/follow' + (want ? '?session=' + encodeURIComponent(want) : ''), { cache: 'no-store' });
        if (r.ok) {
          const f = (await r.json()).session;
          if (want === locked) { // else the user switched meanwhile: drop it
            if (!locked && f && f.running) locked = f.session_id;
            render(f);
          }
        } else if (r.status === 404 && !picking) {
          container.innerHTML = '<div class="sg-empty"><strong>Update softgaze</strong>This server is too old.</div>';
        }
      } catch (e) { /* offline: the SSE handler reports it */ }
      inflight = false;
      last = Date.now();
      if (again) { again = false; load(); }
    }
    // At most a couple of fetches per second, however busy the stream is.
    // An event loads at once when the last fetch is old enough, so updates
    // never wait on a timer; a timer only coalesces bursts.
    function soon() {
      const wait = 400 - (Date.now() - last);
      if (wait <= 0) { if (timer) { W.clearTimeout(timer); timer = null; } load(); return; }
      if (!timer) timer = W.setTimeout(() => { timer = null; load(); }, wait);
    }

    // In the VS Code extension's panel (an iframe): the server may ask it to
    // come forward (Ctrl+Alt+J in any VS Code window, where softgaze
    // cannot raise windows itself), which the extension does.
    const embedded = window.top !== window;
    function connect() {
      if (stopped) return;
      es = new EventSource('/api/events' + (embedded ? '?embed=vscode' : ''));
      if (embedded) {
        es.addEventListener('focus_back', () => window.parent.postMessage({ type: 'softgaze:reveal' }, '*'));
      }
      es.onopen = () => { retry = 2; offline = false; load(); };
      es.onerror = () => {
        offline = true;
        soon();
        if (es.readyState === EventSource.CLOSED) {
          W.setTimeout(connect, Math.min(retry * 1000, 30000));
          retry = Math.min(retry * 2, 30);
        }
      };
      es.addEventListener('session_activity', soon);
      es.addEventListener('attention', soon); // you moved to or from the session's window
    }

    async function openPicker() {
      if (picking) return;
      picking = true;
      shown = '';
      container.className = 'sg-root sg-picking' + (evening() ? ' sg-evening' : '');
      container.innerHTML = '<div class="sg-empty">Looking for sessions...</div>';
      choices = [];
      pick = { list: choices, current: null, since: null, breakMin: 0, learned: false, all: false };
      try {
        const r = await fetch('/api/follow/sessions', { cache: 'no-store' });
        if (r.ok) {
          const d = await r.json();
          choices = d.sessions || [];
          pick = { list: choices, current: d.current ? new Set(d.current) : null, since: d.since,
            breakMin: d.break_minutes || 0, learned: !!d.learned, all: false, minutes: d.focused_minutes || 0 };
        }
      } catch (e) { /* shown as an empty list */ }
      if (!picking || stopped) return;
      showPicker('.sg-current');
      doc.title = 'Switch session · Softgaze';
    }
    function showPicker(focusSel) {
      container.innerHTML = pickerHTML(pick, locked);
      const el = container.querySelector(focusSel) || container.querySelector('.sg-row, .sg-cancel');
      if (el) el.focus();
    }
    // Back to the live view: of the chosen session, or of the same one.
    function closePicker(id) {
      if (!picking) return;
      picking = false;
      const chosen = id && id !== locked && choices.find(s => s.session_id === id);
      if (chosen) locked = id;
      if (chosen || latest !== undefined) render(chosen || latest);
      load();
    }
    // Your turn: a click anywhere else in the window brings up the session's
    // VS Code window.
    let state = '', notice = '';
    // F: full screen. In the VS Code extension's window the page is framed
    // and cannot take the screen itself, so it asks the extension, which
    // runs VS Code's Toggle Full Screen on that window. A browser window
    // uses the browser's own full screen.
    function toggleFullScreen() {
      if (window.top !== window) { window.parent.postMessage({ type: 'softgaze:fullscreen' }, '*'); return; }
      if (doc.fullscreenElement) doc.exitFullscreen().catch(() => {});
      else if (doc.documentElement.requestFullscreen) doc.documentElement.requestFullscreen().catch(() => {});
    }
    async function goToEditor() {
      let msg = '';
      try {
        const r = await fetch('/api/follow/focus', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session: locked }),
        });
        if (!r.ok) msg = ((await r.json().catch(() => ({}))).error) || 'Could not open VS Code.';
      } catch (e) { msg = 'Could not reach softgaze.'; }
      if (!msg) return;
      notice = msg;
      shown = '';
      render(latest);
      W.setTimeout(() => { notice = ''; shown = ''; if (!picking && !stopped) render(latest); }, 4000);
    }
    // The click that gives the window focus (to use the keys) only does
    // that: it does not also send you to VS Code. Links and buttons act at
    // once.
    let focusedAt = 0, focusClick = false;
    // The page tells softgaze when it has the keyboard, and again every
    // 20 s while it does: where the system gives no window titles (macOS),
    // that is how the counter knows you are here and not away.
    const pageId = Math.random().toString(36).slice(2, 14) || 'page';
    let told = false;
    function tellFocus(has) {
      if (!has && !told) return;
      told = has;
      fetch('/api/attention/page', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ page: pageId, focused: has }), keepalive: true,
      }).catch(() => { /* offline: the SSE handler reports it */ });
    }
    // The keys stay out of sight: ? shows them, ? or Esc hides them, and
    // they go when the window loses the keyboard (they only work with it).
    const keysShown = () => doc.documentElement.classList.contains('sg-show-keys');
    const showKeys = on => {
      if (on === keysShown()) return;
      doc.documentElement.classList.toggle('sg-show-keys', on);
      fit(); // the line takes room: drop what no longer fits
    };
    const onFocus = () => { focusedAt = Date.now(); tellFocus(true); };
    const onBlur = () => { showKeys(false); tellFocus(false); };
    const onDown = () => { focusClick = !doc.hasFocus() || Date.now() - focusedAt < 300; };
    if (win) { win.addEventListener('focus', onFocus); win.addEventListener('blur', onBlur); }
    container.addEventListener('pointerdown', onDown, true);
    if (doc.hasFocus()) tellFocus(true);
    function onClick(e) {
      const t = e.target.closest && e.target.closest('[data-sg-act]');
      if (!t && !picking && GO[state] && locked) { if (!focusClick) goToEditor(); return; }
      if (!t || !container.contains(t)) return;
      const act = t.dataset.sgAct;
      if (act === 'pick') openPicker();
      else if (act === 'editor' && locked) goToEditor();
      else if (act === 'more' && picking && pick) { pick.all = true; showPicker('.sg-older'); }
      else if (act === 'cancel') closePicker();
      else if (act === 'choose') closePicker(t.dataset.sgId);
    }
    // Keys: S opens the switch list, arrows step through the sessions of the
    // work period, Enter goes to VS Code, F toggles full screen, ? shows or
    // hides the keys; in the list, 1-9 pick a row.
    // Ctrl+Alt+J goes to VS Code too: the same key brings you back (the
    // extension's Softgaze: Toggle), so one key goes both ways.
    function onKey(e) {
      if (e.ctrlKey && e.altKey && !e.metaKey && !e.shiftKey && (e.code === 'KeyJ' || e.key.toLowerCase() === 'j')) {
        if (locked) { e.preventDefault(); goToEditor(); }
        return;
      }
      if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return;
      const onButton = e.target && e.target.closest && e.target.closest('button');
      if (picking) {
        if (e.key === 'Escape') { e.preventDefault(); closePicker(); return; }
        if (/^[1-9]$/.test(e.key)) {
          const row = container.querySelectorAll('.sg-row')[+e.key - 1];
          if (row) { e.preventDefault(); closePicker(row.dataset.sgId); }
          return;
        }
        if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
          e.preventDefault();
          const items = [...container.querySelectorAll('.sg-row, .sg-more')];
          let i = items.indexOf(doc.activeElement);
          i = e.key === 'ArrowDown' ? Math.min(items.length - 1, i + 1) : Math.max(0, i - 1);
          if (items[i]) items[i].focus();
        }
        return;
      }
      const k = e.key.length === 1 ? e.key.toLowerCase() : e.key;
      if (k === 's') { e.preventDefault(); openPicker(); }
      else if (k === 'f') { e.preventDefault(); toggleFullScreen(); }
      else if (k === 'ArrowRight' || k === 'ArrowLeft') { e.preventDefault(); step(k === 'ArrowRight' ? 1 : -1); }
      else if (k === 'Enter' && !onButton && locked) { e.preventDefault(); goToEditor(); }
      else if (k === '?') { e.preventDefault(); showKeys(!keysShown()); }
      else if (k === 'Escape') showKeys(false);
    }
    // The previous or next session of the current work period (all running
    // ones when that has fewer than two), in the switch list's order.
    let stepping = false;
    async function step(dir) {
      if (stepping) return;
      stepping = true;
      try {
        const r = await fetch('/api/follow/sessions', { cache: 'no-store' });
        if (!r.ok) return;
        const d = await r.json();
        const all = d.sessions || [], cur = new Set(d.current || []);
        let list = all.filter(s => cur.has(s.session_id) || s.session_id === locked);
        if (list.length < 2) list = all;
        if (list.length < 2 || picking || stopped) return;
        const i = list.findIndex(s => s.session_id === locked);
        const next = list[(i + dir + list.length) % list.length];
        if (!next || next.session_id === locked) return;
        locked = next.session_id;
        shown = '';
        render(next);
        load();
      } catch (e) { /* offline: the SSE handler reports it */ } finally { stepping = false; }
    }
    container.addEventListener('click', onClick);
    doc.addEventListener('keydown', onKey);

    // Minutes move on their own; checked every 20 s, written only on change.
    const tick = W.setInterval(() => {
      const now = Date.now();
      if (doc.hasFocus()) tellFocus(true);
      container.querySelectorAll('[data-sg-since]').forEach(el => {
        const t = mins(now - ms(el.dataset.sgSince));
        if (el.textContent !== t) el.textContent = t;
      });
      if (!picking) paintFocus();
      if (!picking && latest !== undefined && evening() !== container.classList.contains('sg-evening')) { shown = ''; render(latest); }
      // Claude has gone quiet in one long step (no new events): show the
      // tools line once it is due.
      // (Not while a message is held: the tools line stays off until it goes.)
      if (!picking && !holdTimer && latest && latest.tools && latest.state === 'working' && !container.querySelector('.sg-tools')
          && now - ms(latest.headline_at) > TOOLS_AFTER) { shown = ''; render(latest); }
    }, 20000);
    // Safety net for a missed event (e.g. a status change during a reconnect).
    const poll = W.setInterval(soon, 30000);
    // Back from a sleeping tab or a hidden window: catch up at once.
    const wake = () => { if (!doc.hidden) soon(); };
    doc.addEventListener('visibilitychange', wake);
    if (win) win.addEventListener('focus', wake);

    connect();
    load();
    return {
      session: () => locked,
      destroy() {
        stopped = true;
        W.clearInterval(tick);
        W.clearInterval(poll);
        W.clearTimeout(timer);
        W.clearTimeout(holdTimer);
        doc.removeEventListener('visibilitychange', wake);
        if (win) win.removeEventListener('focus', wake);
        if (win) win.removeEventListener('resize', fit);
        container.removeEventListener('click', onClick);
        container.removeEventListener('pointerdown', onDown, true);
        if (win) { win.removeEventListener('focus', onFocus); win.removeEventListener('blur', onBlur); }
        doc.documentElement.classList.remove('sg-show-keys');
        tellFocus(false);
        doc.removeEventListener('keydown', onKey);
        if (es) es.close();
      },
    };
  }

  // _test: the pure renderers, for webtest/follow.test.mjs.
  window.Softgaze = { mount, _test: { view, pickerHTML, hold, readFor } };
})();
