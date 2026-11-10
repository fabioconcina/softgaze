# softgaze

A calm window to stay with one Claude Code session while it works. A small
Go server plus a VS Code extension; see README.md for the layout. A sibling
of claumon (github.com/fabioconcina/claumon), released the same way.

## Build & Test

```bash
make build   # ./softgaze
make test    # go test ./... and node --test webtest/
make vet
make ext     # dist/softgaze.vsix
```

## Conventions

- Same as claumon: `log.Printf("[tag] ...")`, return errors, `writeJSON`/
  `writeJSONError`, table-driven tests, no CGO.
- Port 3133, data in `~/.softgaze`, service names `softgaze`, VS
  Code extension `fabioconcina.softgaze` with settings under
  `softgaze.*`: nothing may clash with claumon.
- The test for any change is the vision: a calm, meditation-like space to
  stay present with one Claude session.

## Releasing

- The app: write `RELEASE_NOTES.md` (it becomes the GitHub Release body),
  then push a `vX.Y.Z` tag. The release workflow runs goreleaser: binaries
  for macOS, Linux and Windows, SBOMs, an attestation and the Homebrew
  formula in `fabioconcina/homebrew-softgaze`.
- The extension: bump `extension/package.json` (and the lockfile), add a
  section to `extension/CHANGELOG.md`, then push an `ext-vX.Y.Z` tag. The
  workflow attaches the .vsix to a GitHub Release; upload it to the
  Marketplace by hand.
- The default branch is `master`.
