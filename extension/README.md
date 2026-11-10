# Softgaze for VS Code

A quiet window for staying with one Claude Code session while it works. It
keeps your task in sight, shows what Claude says it is doing, and changes
color once, when it is your turn. Nothing ticks, nothing scores you, and the
window never jumps to another session on its own.

This extension opens the window in VS Code and moves between it and the
session with one key.

## Requires the softgaze app

The extension is the VS Code side of
[softgaze](https://github.com/fabioconcina/softgaze), a free, open-source
app that runs on your machine and reads Claude Code's transcripts. Install
it and start it first:

```bash
brew install fabioconcina/softgaze/softgaze
softgaze service install
```

No Homebrew? Download a binary for macOS, Linux or Windows from the
[latest release](https://github.com/fabioconcina/softgaze/releases/latest).
More in the [install notes](https://github.com/fabioconcina/softgaze#install).

Everything stays on your machine: the extension only talks to softgaze at
`127.0.0.1:3133`.

## Commands

- **Softgaze: Open**: open the window. It gets a VS Code window of its own,
  with a locked editor group so files never open beside it.
- **Softgaze: Toggle** (`Ctrl+Alt+J`): from VS Code back to Softgaze. In
  Softgaze, the same key (or Enter) goes to the session's VS Code window
  and its Claude Code tab.

If `Ctrl+Alt+J` is taken, change it under **Keyboard Shortcuts** (search
for "Softgaze").

## Settings

- `softgaze.port`: the port of the softgaze app on this machine (3133 by
  default). Change it only if you changed `port` in
  `~/.softgaze/config.json`.

## What the extension reports

Each VS Code window tells the softgaze app on your machine whether it has
the keyboard, which folders it has open and whether its Softgaze panel is
active. macOS does not say which window of an app is in front, and this is
how the minutes counter tells your project's window from any other. The
reports are kept in memory, never logged, and never leave your machine.
