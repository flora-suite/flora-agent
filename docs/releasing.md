# Releasing flora-agent

Publishing a Git tag in the form `vX.Y.Z` starts a stable release. A tag such as
`vX.Y.Z-rc` or `vX.Y.Z-rc.1` starts a GitHub pre-release. Both build
static binaries for Linux, macOS, and Windows on both amd64 and arm64, creates
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

Stable releases update these public repositories after a successful release:

- Homebrew Formula: `flora-suite/homebrew-flora`, `Formula/flora-agent.rb`
- Scoop bucket: `flora-suite/scoop-flora`, `bucket/flora-agent.json`

Configure these repository secrets in `flora-suite/flora-agent` before the first
release:

| Secret | Required access |
| --- | --- |
| `HOMEBREW_TAP_TOKEN` | Fine-grained token with **Contents: Read and write** for `flora-suite/homebrew-flora` |
| `SCOOP_BUCKET_TOKEN` | Fine-grained token with **Contents: Read and write** for `flora-suite/scoop-flora` |

If either secret is absent, the GitHub Release still succeeds and that package
repository update is deliberately skipped. The next tag release will synchronize
it once the secret is configured.

Pre-releases never update Homebrew or Scoop, ensuring preview builds cannot
replace the stable installation channels.

The updater scripts can also be run locally after a release exists:

```bash
scripts/update-homebrew-formula.sh /path/to/homebrew-flora 0.1.0
scripts/update-scoop-manifest.sh /path/to/scoop-flora 0.1.0
```
