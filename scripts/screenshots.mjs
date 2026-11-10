// Retakes the README screenshots in assets/: the real page (web/) against a
// mock API with invented sessions, in headless Chromium, so nothing in them
// comes from a real transcript. Run with: make screenshots
import http from 'node:http';
import { readFileSync, realpathSync, mkdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// playwright-core is not a dependency of this repo: `make screenshots` runs
// this under npx, which puts its command on the PATH. Load the package from
// there.
const cli = realpathSync(execFileSync('which', ['playwright-core'], { encoding: 'utf8' }).trim());
const { chromium } = createRequire(import.meta.url)(dirname(cli));
const root = dirname(dirname(fileURLToPath(import.meta.url)));
const web = root + '/web', out = root + '/assets';

const ago = min => new Date(Date.now() - min * 60000).toISOString();
const base = {
  session_id: 'a1', project: 'orchard-api', cwd: '/home/sam/code/orchard-api', running: true,
  task: 'Add rate limiting to the public endpoints, with tests',
  prompt: 'Add rate limiting to the public endpoints, with tests',
  entrypoint: 'claude-vscode', focus: { since: ago(38), away_ms: 0, away_since: '' },
};
const frames = {
  working: { ...base, state: 'working', turn_start: ago(6), headline_at: ago(0.2),
    headline: 'The limiter is in place as middleware. Now I am wiring it to the three public routes and adding a test for the 429 response.',
    trail: ['I will start by reading how requests reach the handlers.', { kind: 'tools', text: 'Read, Grep' },
      'The router has no middleware chain yet, so I will add a small one.'],
    tools: 'Edit, Bash', activity: 'thinking', doing: 'Run the router tests' },
  'needs-you': { ...base, state: 'needs_you', turn_start: ago(9), headline_at: ago(1), headline: '',
    trail: ['The limiter is wired to the three public routes.', 'The tests need Redis running locally.'],
    needs_you: { kind: 'approval', text: 'docker compose up -d redis', options: ['Yes', "Yes, and don't ask again", 'No'], context: 'Start Redis for the integration tests' } },
  finished: { ...base, state: 'finished', turn_start: ago(14), turn_end: ago(1), headline_at: ago(1),
    headline: 'Rate limiting is in: 60 requests a minute per key on the three public routes, with a 429 and a Retry-After header beyond that. All 42 tests pass.',
    trail: ['The limiter is wired to the three public routes.', { kind: 'tools', text: 'Bash, Edit' }, 'Two tests failed on the header name; fixed.'] },
};
const others = [
  { session_id: 'b2', project: 'orchard-web', cwd: '/home/sam/code/orchard-web', running: true, state: 'finished',
    task: 'Make the pricing table readable on phones', headline: 'The table now stacks below 600 px. Have a look at the preview.' },
  { session_id: 'c3', project: 'field-notes', cwd: '/home/sam/code/field-notes', running: true, state: 'needs_you',
    task: 'Import last season\'s notes from the CSV export', headline: 'Which date format does the export use?' },
];
let current = 'working';
const types = { html: 'text/html', js: 'text/javascript', png: 'image/png' };
const srv = http.createServer((req, res) => {
  const p = new URL(req.url, 'http://x').pathname;
  const json = o => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(o)); };
  if (p === '/api/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(': hi\n\n'); return; }
  if (p === '/api/follow') return json({ session: frames[current] });
  if (p === '/api/follow/sessions') return json({ sessions: [frames[current], ...others], current: ['a1', 'b2', 'c3'], since: ago(95), break_minutes: 65, learned: true, focused_minutes: 132 });
  if (p === '/api/follow/pin') return json({ supported: false });
  if (p.startsWith('/api/')) return json({});
  const file = p === '/' ? 'follow.html' : p.slice(1);
  try { const b = readFileSync(web + '/' + file); res.writeHead(200, { 'Content-Type': types[file.split('.').pop()] || 'text/plain' }); res.end(b); }
  catch { res.writeHead(404); res.end(); }
});
await new Promise(r => srv.listen(0, '127.0.0.1', r));
const url = `http://127.0.0.1:${srv.address().port}/follow.html`;
mkdirSync(out, { recursive: true });
const browser = await chromium.launch();
const shot = async (name, after) => {
  current = name === 'switch' ? 'working' : name;
  const page = await browser.newPage({ viewport: { width: 760, height: 320 }, deviceScaleFactor: 2 });
  await page.goto(url);
  await page.waitForSelector('.sg-headline');
  await page.waitForTimeout(600);
  if (after) await after(page);
  await page.screenshot({ path: `${out}/${name}.png` });
  await page.close();
};
for (const n of Object.keys(frames)) await shot(n);
await shot('switch', async page => { await page.keyboard.press('s'); await page.waitForSelector('.sg-row'); await page.waitForTimeout(300); });
await browser.close();
srv.close(); process.exit(0);
