# Install GoSync Desktop

[← GoSync](../readme.md) · [Desktop guide](DESKTOP.md) · [Releases](RELEASING.md)

Copy this command into Terminal on each computer and press Enter:

```sh
curl -fsSL https://raw.githubusercontent.com/TypeTerrors/go_sync/main/install.sh | bash
```

Supported: **macOS 12+** and **Ubuntu Desktop 24.04+**, Intel/AMD 64-bit and ARM64.
No Go installation, Homebrew, peer addresses or pairing codes are required.
The installer downloads the newest compatible published desktop release, verifies
its SHA-256 checksum and installs it. Run as your regular desktop user, without
putting `sudo` before the command. Ubuntu requests administrator access only for
its normal package manager. macOS installs into `~/Applications/GoSync.app`.

GoSync opens after installation when a graphical session is available. Choose a
folder or select an existing configuration, then read and confirm the app's setup
prompt. The installer does not initialize a folder, reset history, or activate the
synchronization service itself. Canceling setup leaves your files alone.

**Automatic LAN sync is unencrypted and unauthenticated.** Existing files are
shared and changes, including deletions, propagate between peers in the same LAN
group. Use a trusted network and keep independent backups. Existing mutual-TLS
configurations retain their security settings. See [first launch](DESKTOP.md#first-launch).

## Platform notes

**Mac:** builds are ad-hoc signed, not Developer ID signed or notarized. If
Gatekeeper blocks the app, verify the source and use **System Settings → Privacy
& Security → Open Anyway** where available. Managed Macs can prohibit exceptions.
The installer never disables Gatekeeper or removes quarantine attributes. The
menu-bar icon has no Dock icon. Allow local-network access if prompted.

**Ubuntu:** the `.deb` installs the GTK dashboard, dialog, and session-bus runtime
dependencies through `apt`. Ubuntu's AppIndicator extension must be enabled to
show the tray icon; standard Ubuntu Desktop normally includes it. If missing,
install `gnome-shell-extension-appindicator` with the package manager and enable
it in Extensions. The installer does not install recommended desktop extensions
automatically. A graphical session and systemd user services are required to run
the app; installation in a server/container does not create a desktop session.
If `curl` is missing, install it with Ubuntu's package manager first.

## Updating an existing installation

1. Choose **Stop GoSync**, then **Quit Tray**. Stop any separate CLI daemon too.
2. Run the same installer command.
3. Open GoSync and choose **Start GoSync** when ready.

Updates preserve configuration, synchronized files, security settings, and state.
A running daemon/tray or loaded Mac LaunchAgent blocks replacement. On macOS an
existing `/Applications/GoSync.app` is updated in place if writable. If both the
system and user Applications folders contain GoSync, select the intended one with
`--app-dir`. Administrator-owned apps can be updated manually using Finder.
An unsuccessful Mac app replacement restores the old app where possible; if
restoration cannot complete, the installer prints the retained backup location.

A CLI installation can be adopted through **Settings → Select existing
configuration**, keeping its explicit state directory. See [migration details](DESKTOP.md#controls-and-existing-cli-configurations).
The tray manages one configuration and does not replace custom service units.

## Download first, inspect, then run

Piping a script to Bash executes code from this repository. If you prefer to
inspect it first:

```sh
curl -fsSL https://raw.githubusercontent.com/TypeTerrors/go_sync/main/install.sh -o install-gosync.sh
less install-gosync.sh
bash install-gosync.sh
```

Both methods trust the repository and its HTTPS downloads. A checksum detects a
mismatched or corrupted package; it is not an independent publisher signature.
Manual `.app` and `.deb` downloads remain available in
[GitHub Releases](https://github.com/TypeTerrors/go_sync/releases).

## Options

Pass options to the downloaded script, or append `-s -- OPTIONS` after `bash` in
the copy/paste command.

| Option | Behavior |
|---|---|
| `--no-launch` | Install without opening the tray. Useful for managed deployment. |
| `--version TAG` | Install an exact `v_<6-character SHA>.<hex timestamp>` release. |
| `--app-dir /absolute/directory` | Choose the Mac directory containing `GoSync.app`. |
| `--download-only DIR` | Save the verified native package and checksum; do not install. Existing output files are not overwritten. |
| `--github-auth` | Use the authenticated GitHub CLI for private repository access. |
| `--help` | Show supported options. |

For example, install without opening the app:

```sh
curl -fsSL https://raw.githubusercontent.com/TypeTerrors/go_sync/main/install.sh | bash -s -- --no-launch
```

The installer targets the native architecture, including Apple Silicon when the
Terminal is running under Rosetta. Unsupported distributions and architectures
stop before installation. Ubuntu root automation may use `--no-launch`; normal
desktop users should run without `sudo` so the UI belongs to their own session.

### Private repository access

The public command works after the repository becomes public. While it is
private, collaborators with an authenticated [GitHub CLI](https://cli.github.com/)
can use:

```sh
gh api repos/TypeTerrors/go_sync/contents/install.sh -H 'Accept: application/vnd.github.raw+json' | bash -s -- --github-auth
```

GitHub authentication is used only to read the script and release assets. No
credentials are written to GoSync's configuration or copied to another computer.

## Troubleshooting and removal

- **404 fetching the script:** a private repository is invisible to anonymous
  requests. Use the collaborator method above until public access is enabled.
- **No compatible release:** a release may still be building. Retry once all
  desktop assets are published, or choose an existing complete tag.
- **Checksum/signature failure:** installation stops. Retry the download; report
  a persistent mismatch rather than bypassing verification.
- **GoSync is running:** stop the daemon and quit the tray before updating.
- **Ubuntu package error:** read apt's explanation (permissions, network or package
  locks), fix that condition and rerun. Configuration is not reset.
- **No icon after installation:** open GoSync from Applications; on GNOME check
  the AppIndicator extension. On macOS check Gatekeeper and privacy permissions.

To uninstall, first use **Settings → Remove desktop service and login
integration**. Then remove the Ubuntu package with the graphical package manager
or trash `GoSync.app`. Configuration, synchronized files, history and logs remain.
[Service lifecycle and removal details](DESKTOP.md#background-lifecycle-and-removal).
