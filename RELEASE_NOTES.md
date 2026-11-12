## Easier long waits

This release is about the long stretches while Claude works: the window
now says what Claude is doing when it goes quiet, and gives you a place to
set a thought down.

- **A note while you wait.** In a long wait the bottom line asks
  "Something on your mind? Note it". Press N and write one line, so you
  can let the thought go. At your turn the note is set apart, with
  **Copy** and **Done**; until you mark it done it comes back turn after
  turn. One note per session, kept in memory only, never typed into
  Claude Code.
- **A long silence says what Claude is doing.** After half a minute with
  nothing new from Claude, a quiet line names the step that is running
  ("Run the test suite · 6 min so far") or the kinds of steps since its
  last words. After 15 minutes with nothing new it says Claude may be
  stuck.
- **Cards in the switch list.** Each session is a card with its project,
  state, task and Claude's last words, edged in the session's color. The
  arrow keys move between cards as they are laid out.
- **Tables** in Claude's messages are drawn as tables, not as rows of
  pipes.
- **Keep on top is gone.** The window no longer floats above other
  windows: no Picture-in-Picture in Chrome and Edge, and no always on top
  in other browsers, where nothing could undo it.

To update:

```bash
brew upgrade softgaze
softgaze service restart
```

Without Homebrew, replace the binary with the new one below and run
`softgaze service restart`. Copy on a note inside the VS Code panel needs
the Softgaze extension 0.1.1.
