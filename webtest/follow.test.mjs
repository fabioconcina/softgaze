// The window's renderers never turn transcript text into markup: every
// field softgaze shows (all of it from Claude's transcripts) is escaped.
// Run with: node --test webtest/
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const src = readFileSync(new URL('../web/follow.js', import.meta.url), 'utf8');

function renderers() {
  const window = {};
  vm.runInContext(src, vm.createContext({ window, navigator: { userAgent: 'node' } }));
  return window.Softgaze._test;
}

// Markup, an attribute break-out, and a list line for Claude's messages.
const P = `<img src=x onerror=alert(1)>"'><svg onload=alert(2)> x" onmouseover="alert(3)`;
const ESCAPED = '&lt;img src=x onerror=alert(1)&gt;';

function assertInert(html, what) {
  assert.doesNotMatch(html, /<img|<svg/i, `${what}: markup got through`);
  assert.doesNotMatch(html, /["']\s*onmouseover/i, `${what}: an attribute got through`);
  assert.ok(html.includes(ESCAPED), `${what}: the text was not shown at all`);
}

function session(state, extra = {}) {
  return {
    session_id: 's-' + state, state, project: P, cwd: P, task: P, prompt: P, running: true,
    headline: `${P}\n\n• ${P}\n  2. ${P}\n| ${P} | x |\n| y \\| z | ${P} |`, headline_at: P, turn_start: P, turn_end: P,
    trail: [P, { kind: 'tools', text: P }, { kind: 'msg', text: P }],
    tools: P, activity: P, unsure: P, at_it_since: P,
    background: { what: P, since: P, count: 2 },
    focus: { since: P, away_ms: P, away_since: P },
    ...extra,
  };
}

test('every state escapes every field', () => {
  const { view } = renderers();
  const cases = {
    working: session('working'),
    'working, no narration yet': session('working', { headline: '' }),
    'working, a newer message waiting': session('working', { held: true }),
    'needs you (approval)': session('needs_you', { needs_you: { kind: 'approval', text: P, options: [P, P], context: P } }),
    'needs you (question)': session('needs_you', { needs_you: { kind: 'question', text: P, options: [P], context: P } }),
    finished: session('finished', { needs_you: { kind: 'reply', text: P } }),
    'finished, one line': session('finished', { headline: P }),
    background: session('background', { headline: '' }),
    unsure: session('unsure'),
    ended: session('ended'),
    empty: session('empty'),
  };
  for (const [name, f] of Object.entries(cases)) {
    assertInert(view(f).html, name);
  }
});

test('the switch list escapes every field', () => {
  const { pickerHTML } = renderers();
  const list = [session('working'), session('finished'), session('needs_you', { state: P }),
    session('ended', { headline: `x\n| ${P} | y |` })];
  const pick = { list, current: new Set(['s-working']), since: P, breakMin: 120, learned: true, all: false, minutes: 30 };
  assertInert(pickerHTML(pick, 's-working'), 'current work period');
  assertInert(pickerHTML({ ...pick, all: true }, 's-working'), 'all sessions');
});

// While Claude works, its message stays until it has had its reading time;
// a newer one waits for it. Anything else shows at once.
test('a message is held for its reading time', () => {
  const { hold, readFor, view } = renderers();
  const msg = (at, headline, extra = {}) => ({
    session_id: 's', state: 'working', project: 'p', turn_start: 't1', headline_at: at, headline,
    trail: [at + ' before'], tools: 'Bash', ...extra,
  });
  const first = hold(null, msg('a', 'one two three four'), 1000);
  assert.equal(first.frame.headline, 'one two three four');
  assert.equal(first.until, 0);
  assert.equal(first.onScreen.since, 1000);
  const due = 1000 + readFor('one two three four');
  assert.equal(due, 1000 + 3000 + 4 * 250);

  // A newer message before then: the first stays, with its own trail.
  const held = hold(first.onScreen, msg('b', 'newer'), 2000);
  assert.equal(held.frame.headline, 'one two three four');
  assert.deepEqual([...held.frame.trail], ['a before']);
  assert.equal(held.frame.held, true);
  assert.equal(held.frame.tools, '');
  assert.equal(held.until, due);
  assert.match(view(held.frame).html, /sg-newer/);
  assert.doesNotMatch(view(first.frame).html, /sg-newer/);

  // Once read, the newest shows (one in between is skipped), and its own
  // reading time starts.
  const next = hold(held.onScreen, msg('c', 'newest'), due + 1);
  assert.equal(next.frame.headline, 'newest');
  assert.equal(next.frame.held, undefined);
  assert.equal(next.onScreen.since, due + 1);

  // The same message growing shows at once and keeps its start.
  const grown = hold(first.onScreen, msg('a', 'one two three four five'), 2000);
  assert.equal(grown.frame.headline, 'one two three four five');
  assert.equal(grown.until, 0);
  assert.equal(grown.onScreen.since, 1000);

  // Claude needs you, stops, or a new turn or another session: no wait.
  for (const f of [msg('b', 'x', { state: 'needs_you' }), msg('b', 'x', { state: 'finished' }),
    msg('b', 'x', { turn_start: 't2' }), msg('b', 'x', { session_id: 'other' })]) {
    const h = hold(first.onScreen, f, 2000);
    assert.equal(h.frame, f, f.state + ' ' + f.turn_start + ' ' + f.session_id);
    assert.equal(h.until, 0);
  }
  // A working turn with nothing said yet clears what was on screen.
  assert.equal(hold(first.onScreen, msg('', ''), 2000).onScreen, null);
});

test('reading time grows with the message, up to a cap', () => {
  const { readFor } = renderers();
  assert.equal(readFor(''), 3000);
  assert.equal(readFor('a b'), 3500);
  assert.equal(readFor('w '.repeat(1000)), 45000);
});

// Claude has said nothing yet this turn: the tool it runs, or between tools
// the one it last ran; "starting on it" only before the first tool.
test('a silent turn shows what Claude last set out to do', () => {
  const { view } = renderers();
  const base = { session_id: 's', state: 'working', project: 'p', turn_start: 't1', headline: '', trail: [] };
  const headline = f => /<div class="sg-headline">(.*?)<\/div>/s.exec(view(f).html)[1];
  assert.match(headline({ ...base, activity: 'thinking' }), /Claude is starting on it\./);
  assert.match(headline({ ...base, activity: 'List open PRs', doing: 'List open PRs' }), /List open PRs/);
  const between = headline({ ...base, activity: 'thinking', doing: 'writing notes.md' });
  assert.match(between, /writing notes\.md/);
  assert.doesNotMatch(between, /starting on it/);
});

// Claude's tables arrive as "| a | b |" rows (softgaze drops the |---|
// line): a run of them is a table, its first row the head. A message cut to
// fit keeps its "…" outside the table.
test('table rows become a table', () => {
  const { view, pickerHTML } = renderers();
  const headline = text => /<div class="sg-headline">(.*)<\/div>/s.exec(view({
    session_id: 's', state: 'working', project: 'p', turn_start: 't1', headline: text, trail: [],
  }).html)[1];
  const t = headline('Checked:\n| Request | Result |\n| No header | 403 |\n| a \\| b | 401 |\nDone.');
  assert.match(t, /<div>Checked:<\/div><div class="sg-tw"><table class="sg-table"><thead><tr><th>Request<\/th><th>Result<\/th><\/tr><\/thead>/);
  assert.match(t, /<tbody><tr><td>No header<\/td><td>403<\/td><\/tr><tr><td>a \| b<\/td><td>401<\/td><\/tr><\/tbody><\/table><\/div><div>Done\.<\/div>/);
  // Cut at the end, and at the start (no head: the rows are the rest of a table).
  assert.match(headline('Before\n| A | B |\n| 1 | 2 | …'), /<td>2<\/td><\/tr><\/tbody><\/table><\/div><div>…<\/div>/);
  const rest = headline('…| 3 | 4 |\n| 5 | 6 |\nNext.');
  assert.match(rest, /^<div>…<\/div><div class="sg-tw"><table class="sg-table"><tbody><tr><td>3<\/td>/);
  assert.doesNotMatch(rest, /<th>/);
  // Earlier messages and the switch list have no room for a table: a row
  // reads as its cells in a line.
  const trail = view({ session_id: 's', state: 'working', project: 'p', turn_start: 't1', headline: 'x',
    trail: ['| A | B |\n| 1 | 2 |', { kind: 'msg', text: 'See:\n| C | D |' }] }).html;
  assert.match(trail, /<div class="sg-trail"><div>A · B\n1 · 2<\/div><div>See:\nC · D<\/div><\/div>/);
  const pick = { list: [{ session_id: 's', state: 'finished', project: 'p', headline: 'x\n| The page | 302 |' }], all: true };
  assert.match(pickerHTML(pick, 'other'), /<span class="sg-row-last">The page · 302<\/span>/);
});
