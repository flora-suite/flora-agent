# Releasing flora-agent

Publishing a Git tag in the form `vX.Y.Z` starts a stable release. A tag such as
`vX.Y.Z-rc` or `vX.Y.Z-rc.1` starts a GitHub pre-release. Both build
static binaries for Linux and macOS on both amd64 and arm64, create
archives and `checksums.txt`, and attaches them to the corresponding GitHub
Release.

```bash
git tag v0.1.0
git push origin v0.1.0
```

Pre-release example:

```bash
git tag v0.1.0-rc
git push origin v0.1.0-rc
```

The workflow injects the release version, commit SHA, and UTC build date into
`flora-agent version`.

## Package repository synchronization

Every release updates the Homebrew tap after a successful release, using a
separate Formula channel:

- Stable tags update `flora-suite/homebrew-flora`, `Formula/flora-agent.rb`.
- Pre-release tags update `flora-suite/homebrew-flora`,
  `Formula/flora-agent@rc.rb`. This Formula is keg-only, so it cannot replace
  the stable executable without an explicit `brew link --overwrite flora-agent@rc`.

Users install the stable channel with `brew install flora-agent`. To opt into a
release candidate, they must explicitly run `brew install flora-agent@rc`.

Configure these repository secrets in `flora-suite/flora-agent` before the first
release:

| Secret | Required access |
| --- | --- |
| `HOMEBREW_TAP_TOKEN` | Fine-grained token with **Contents: Read and write** for `flora-suite/homebrew-flora` |

If the secret is absent, the GitHub Release still succeeds and the package
repository update is deliberately skipped. The next tag release will synchronize
it once the secret is configured.

The updater scripts can also be run locally after a release exists:

```bash
scripts/update-homebrew-formula.sh /path/to/homebrew-flora 0.1.0 stable
scripts/update-homebrew-formula.sh /path/to/homebrew-flora 0.1.0-rc.2 rc
```
