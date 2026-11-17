## Your sessions, your way

This release is about the sessions themselves: what they are called, which
ones you see, and how Claude's messages read in the window.

- **Names for sessions.** A session is shown by its project until you name
  it: **Rename** on its card in the switch list (or R). The name takes the
  project's place in the window, the list and the title, with the project
  beside it. A name given with Claude Code's `/rename` shows too. Names are
  kept across restarts.
- **Set a session aside.** A session you keep open but are done with for
  now can leave the switch list and the arrow keys: **Set aside** on its
  card (or X). It comes back by itself when you prompt it again. Set aside
  the session the window follows and the window follows none, waiting for
  your next prompt instead of jumping to another one.
- **Quotes and inline code** in Claude's messages. A quoted block (often a
  draft for you to pass on) is drawn as a quote, with its lists laid out,
  and `code` shows in a quiet monospace. Names like
  `mcp__server__tool` keep their underscores.

To update:

```bash
brew upgrade softgaze
softgaze service restart
```

Without Homebrew, replace the binary with the new one below and run
`softgaze service restart`. No extension update is needed.
