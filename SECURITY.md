# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/fabioconcina/softgaze/security/advisories/new).
Don't open a public issue for anything exploitable.

I'll acknowledge reports within a few days and keep you posted until a fix
ships. Credit in the release notes is yours if you want it.

## Supported versions

Only the latest release of the `softgaze` binary and the latest version of
the VS Code extension receive fixes. Update with `brew upgrade softgaze`.

## Scope

softgaze reads your Claude Code session transcripts and serves what they
say on a local page. It can also bring windows to the front. Issues that
are in scope include:

- anything that lets a web page, another user, or a remote host read that
  data or trigger the window's actions (focusing windows)
- softgaze making a network request to any other host
- tampering with release artifacts
- the VS Code extension being redirected or abused by an untrusted workspace

Out of scope: attacks that already require code execution as your user, and
exposing the port to a network yourself.
