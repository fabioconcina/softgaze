## First release

softgaze is a quiet window for staying with one Claude Code session while
it works. It keeps your task in sight, shows what Claude says it is doing,
and changes color once, when it is your turn.

- **One session at a time.** The window locks onto the session you prompt
  and stays there until you switch.
- **Five plain states**: working, needs you, finished, in the background,
  session closed. When softgaze cannot read what Claude Code wrote, it says
  so instead of guessing.
- **Work periods.** The switch list shows the sessions of your current
  stretch of work first, learned from your own history.
- **Wellbeing, on by default**: minutes with the session, one break line
  after 90 minutes, today's minutes per session. No streaks, no scores.
- **Local only.** It listens on `127.0.0.1`, makes no network calls, and
  never writes to `~/.claude`.

Install:

```bash
brew install fabioconcina/softgaze/softgaze
softgaze service install
code --install-extension fabioconcina.softgaze
```

Binaries for macOS, Linux and Windows are below.
