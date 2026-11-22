# Merge tags and GitHub releases

[← Back to GoSync](../readme.md)

The [release workflow](../.github/workflows/TagReleaseBuildPush.yaml) tags merged
GitHub pull requests. A merge into the repository's default branch also publishes
downloadable binaries in [GitHub Releases](https://github.com/TypeTerrors/go_sync/releases).
The default branch is currently `main`; the same rule applies if it is renamed to
`master` later.

| Event | Result |
|:---|:---|
| PR merged into any other branch | Tag at that PR's merged commit |
| PR merged into the default branch | Tag, then tests, builds, and a release using that tag |
| PR closed without merging | No tag or release |
| Ordinary branch push | Regular regression CI; no automatic tag or release |
| Rerun of the same merge workflow | Reuses the tag; resumes an unfinished draft or leaves a complete published release unchanged |

Use GitHub PR merges to drive this workflow. A merge performed locally and pushed
without a merged GitHub PR does not produce a PR-closed event. Existing commits
and historical merges are not backfilled.

## Tags and exact source versions

Tags use the project's original `v_<6-character SHA>.<hex Unix timestamp>` format.
Release display names use `v_<6-character SHA>.<YYYYMMDD>`. For example, a merge
of commit `1234567890ab…` at `2026-11-22T00:00:00Z` produces:

- Tag: `v_123456.6b023080`
- Release name: `v_123456.20261122`

The timestamp and UTC date come from the PR's recorded merge time, rather than
the runner's current clock. Delayed builds and reruns therefore reuse the same
tag and release name, including when retried on another day. These identify merge
builds; they are not semantic-version stability promises. The merged PR's
`merge_commit_sha` is used for both the tag and
checkout, covering GitHub merge, squash, and rebase merges. Later pushes cannot
change which source gets built.

The tag is created first, even if subsequent tests or builds fail. A failed build
does not publish a release. An existing tag pointing elsewhere is an error; the
workflow never moves it. Per-PR concurrency prevents overlapping reruns without
cancelling unrelated merges. Previously published tags and releases are retained.

## Release contents and checks

Default-branch merges run the release behavior tests, Go race tests, `go vet`, and
the three-container mDNS/file regression suite before building all six binaries:

| Platform | amd64 / Intel or AMD | arm64 / ARM or Apple Silicon |
|:---|:---|:---|
| Linux | `gosync-linux-amd64` | `gosync-linux-arm64` |
| macOS | `gosync-darwin-amd64` | `gosync-darwin-arm64` |
| Windows | `gosync-windows-amd64.exe` | `gosync-windows-arm64.exe` |

`SHA256SUMS` accompanies the six raw executables. Linux/macOS CLI users also
need to make the executable runnable. Windows CLI builds remain cross-compiled.

Four additional native desktop packages are built in a separate matrix:

| Runner | Desktop download |
|---|---|
| `ubuntu-24.04` | `gosync-desktop-linux-amd64.deb` |
| `ubuntu-24.04-arm` | `gosync-desktop-linux-arm64.deb` |
| `macos-15-intel` | `gosync-desktop-darwin-amd64.zip` |
| `macos-15` (Apple Silicon) | `gosync-desktop-darwin-arm64.zip` |

Every desktop package has its own `.sha256` file. Each matrix job verifies its
native architecture, runs desktop race tests/vet, and packages both the tray and
daemon. macOS bundles use CGO/Cocoa and ad-hoc signatures; Developer ID signing
and notarization need separately configured credentials. See [desktop setup and
validation](DESKTOP.md). PR CI runs the same native build matrix without publishing.
Homebrew and Windows desktop installers remain separate future work.

The publishing job verifies the expected files and their checksums, creates a
draft with generated release notes, uploads all fifteen files, and only then makes
the release public. A serialized publishing step marks it **Latest** only if no
newer complete release exists, comparing the hexadecimal merge timestamps. An
older build finishing late cannot displace a newer release. Tag names stay in the
original SHA/timestamp format.

## Runner and permissions

Jobs use GitHub-hosted Ubuntu/macOS runners and the built-in `GITHUB_TOKEN`.
No custom runner or `ACCESS_TOKEN` secret is required. Repository or organization
policies must permit Actions and the declared `contents: write` permission.

The workflow uses `pull_request_target: closed` with a merged-only condition so
merged contributions from forks can be tagged too. GitHub loads this workflow from
the default branch. Tagging and publishing use inline API calls and never check
out or execute repository code. Tests and compilation run in a separate job with
read-only contents permission, no persisted checkout credentials, and no Go cache.
The publishing job downloads the current run's artifact; it treats the files as
data and never executes them.

GitHub suppresses most new workflow triggers from operations made with
`GITHUB_TOKEN`, so publication deliberately happens in the same workflow as tag
creation. It does not depend on a second tag-push workflow firing.

## Recovering a failed run

Open **Actions → Tag merges and publish releases**, choose the merge run, and
rerun its failed jobs (or all jobs). The same event produces the same tag.

- A failed test or build leaves the tag and no public release.
- An interrupted upload leaves a draft; a rerun replaces draft assets and
  publishes only after all uploads succeed.
- A complete published release is left untouched. Missing public assets,
  unexpected draft assets, or a moved tag require investigation instead of
  automatic replacement.
- Build artifacts expire after seven days. Rerun all jobs to rebuild them if
  retrying publication after that period.

Once this workflow is on the default branch, future PR merges trigger it. Updating
the workflow by a direct push does not itself create a release.

## Local validation

```sh
node --test scripts/release-workflow.test.cjs
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
make dist
make desktop # native host only; see desktop build dependencies
```

The Node tests execute the workflow's actual GitHub API scripts with a simulated
GitHub API and temporary local artifacts. They cover branch selection, exact
commits, duplicate runs, invalid tags, interrupted uploads, incomplete public
releases, and bad checksums. They create no real tags or releases. The ordinary
regression workflow also runs these tests and workflow linting on pushes and PRs.

GitHub references: [merged PR events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#pull_request_target),
[token-trigger behavior](https://docs.github.com/en/actions/how-tos/writing-workflows/choosing-when-your-workflow-runs/triggering-a-workflow),
and the [release API](https://docs.github.com/en/rest/releases/releases).

## Desktop installer

The root [install.sh](../install.sh) is served from `main` for the README's
copy/paste command. Anonymous script and package downloads require a public
repository. Private collaborators can use the authenticated method in the
[installation guide](INSTALLING.md#private-repository-access).

The installer selects the greatest hexadecimal merge timestamp from the most
recent 100 releases that have both the native desktop asset and its checksum.
Drafts, prereleases and incomplete packages are excluded. This deliberately does
not rely on GitHub's **Latest** badge, whose ordering can differ with these
non-semantic-version tags. `--version TAG` selects an exact build instead.

The **Desktop installer** workflow runs contract tests on all four native
platforms, installs a real published Ubuntu package, and installs/replaces the
real macOS app. It uses `--no-launch`; these checks do not exercise GUI onboarding
or Gatekeeper on a clean Mac. It uses the repository-scoped token while the
repository is private, without embedding credentials in the installer.

Local installer checks:

```sh
bash -n install.sh
shellcheck install.sh
python3 scripts/test-installer.py
bash install.sh --github-auth --download-only /tmp/gosync-packages
python3 integration/install.py --packages /tmp/gosync-packages --tag RELEASE_TAG
```

Replace `RELEASE_TAG` with the exact tag printed by the download step. Omit
`--github-auth` for public access. The Docker check runs a real native Ubuntu
package installation with already downloaded assets and records a portable trace.
