#!/usr/bin/env bash
# GoSync desktop installer. Compatible with macOS's Bash 3.2 and Ubuntu Bash.
# The final main call runs only after the script's functions have been received.

say() { printf '  %s\n' "$*"; }
die() { printf '\nGoSync: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "Required command is missing: $1"; }

fetch() {
    curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error \
        --location --retry 3 --connect-timeout 15 --max-time 600 "$@"
}

valid_tag() { printf '%s\n' "$1" | grep -Eq '^v_[0-9a-f]{6}\.[0-9a-f]{1,12}$'; }

# GitHub's Latest badge can point to an old CLI-only release with our non-semver
# tags. Select the greatest recorded merge timestamp with both desktop assets.
# Ubuntu ships Python; macOS uses its built-in JavaScript interpreter instead.
select_release() {
    if [ "$platform" = darwin ]; then
        osascript -l JavaScript - "$work/releases.json" "$asset" <<'JAVASCRIPT'
ObjC.import('Foundation');
function run(args) {
    var text = $.NSString.stringWithContentsOfFileEncodingError(args[0], $.NSUTF8StringEncoding, null);
    var releases = JSON.parse(ObjC.unwrap(text));
    if (!Array.isArray(releases)) throw new Error('Invalid GitHub release response');
    var best = '', newest = -1;
    releases.forEach(function(r) {
        var match = /^v_[0-9a-f]{6}\.([0-9a-f]{1,12})$/.exec(r.tag_name || '');
        if (r.draft || r.prerelease || !match) return;
        var names = (r.assets || []).map(function(a) { return a.name; });
        var stamp = parseInt(match[1], 16);
        if (names.indexOf(args[1]) >= 0 && names.indexOf(args[1] + '.sha256') >= 0 && stamp > newest) {
            best = r.tag_name; newest = stamp;
        }
    });
    if (!best) throw new Error('No complete desktop release found');
    return best;
}
JAVASCRIPT
    else
        python3 - "$work/releases.json" "$asset" <<'PYTHON'
import json, re, sys
with open(sys.argv[1]) as f:
    releases = json.load(f)
if not isinstance(releases, list):
    raise SystemExit('Invalid GitHub release response')
best, newest = '', -1
for r in releases:
    match = re.fullmatch(r'v_[0-9a-f]{6}\.([0-9a-f]{1,12})', r.get('tag_name', ''))
    if r.get('draft') or r.get('prerelease') or not match:
        continue
    names = {a.get('name') for a in r.get('assets', [])}
    stamp = int(match[1], 16)
    if {sys.argv[2], sys.argv[2] + '.sha256'} <= names and stamp > newest:
        best, newest = r['tag_name'], stamp
if not best:
    raise SystemExit('No complete desktop release found')
print(best)
PYTHON
    fi
}

cleanup() {
    # If an interrupted macOS replacement has moved the old app aside, restore
    # it. Never discard a backup if restoration fails or a new path appeared.
    if [ -n "$stage" ] && [ -d "$stage/previous.app" ] && [ "$committed" = false ]; then
        if [ ! -e "$destination" ] && [ ! -L "$destination" ] && mv "$stage/previous.app" "$destination"; then
            say 'Restored the previous application.'
        else
            printf 'Previous application retained at: %s/previous.app\n' "$stage" >&2
            stage=''
        fi
    fi
    [ -z "$stage" ] || rm -rf -- "$stage"
    [ -z "$work" ] || rm -rf -- "$work"
}

check_stopped() {
    need pgrep
    if pgrep -u "$(id -u)" -x 'gosync|gosync-desktop' >/dev/null 2>&1; then
        die 'GoSync is running. Choose Stop GoSync, then Quit Tray, and rerun this installer. Your configuration and history will be retained.'
    fi
    if [ "$platform" = darwin ]; then
        # A loaded job may restart between checks even if it has no PID yet.
        if launchctl print "gui/$(id -u)/com.typeterrors.gosync" >/dev/null 2>&1; then
            die 'The GoSync LaunchAgent is loaded. Stop GoSync in the tray, then quit the tray and rerun.'
        fi
    elif command -v systemctl >/dev/null 2>&1; then
        state=$(systemctl --user show gosync.service -p ActiveState --value 2>/dev/null || true)
        case "$state" in active|activating|reloading|deactivating)
            die 'The GoSync user service is active. Stop GoSync and quit the tray before updating.' ;;
        esac
    fi
}

install_mac() {
    for command in ditto plutil codesign unzip; do need "$command"; done
    if [ -z "$app_dir" ]; then
        if [ -e "$HOME/Applications/GoSync.app" ] && [ -e /Applications/GoSync.app ]; then
            die 'Two GoSync apps exist. Choose the installation to update with --app-dir DIR.'
        elif [ -e /Applications/GoSync.app ]; then
            app_dir=/Applications
        else
            app_dir="$HOME/Applications"
        fi
    fi
    case "$app_dir" in /*) ;; *) die '--app-dir must be an absolute directory path.' ;; esac
    destination="$app_dir/GoSync.app"
    [ ! -L "$destination" ] || die "Refusing to replace a symbolic link: $destination"
    if [ -e "$destination" ]; then
        identifier=$(plutil -extract CFBundleIdentifier raw -o - "$destination/Contents/Info.plist" 2>/dev/null || true)
        [ "$identifier" = com.typeterrors.gosync.desktop ] || die "An unrelated application exists at $destination; it was left untouched."
    fi
    mkdir -p "$app_dir" || die "Cannot create $app_dir. Choose a writable directory with --app-dir."
    [ -w "$app_dir" ] || die "Cannot write $app_dir. Use the manual installation instructions to update this administrator-owned app."
    # The expected archive is the release's complete .app, plus ditto metadata.
    unzip -Z1 "$work/$asset" > "$work/entries" || die 'Cannot inspect application archive.'
    awk '
        /^\// || /(^|\/)\.\.(\/|$)/ { bad=1 }
        !/^GoSync\.app(\/|$)/ && !/^__MACOSX\// { bad=1 }
        END { exit bad }
    ' "$work/entries" || die 'Unexpected paths in the application archive.'
    ditto -x -k "$work/$asset" "$work/unpack" || die 'Cannot unpack the application.'
    candidate="$work/unpack/GoSync.app"
    identifier=$(plutil -extract CFBundleIdentifier raw -o - "$candidate/Contents/Info.plist") || die 'Missing application metadata.'
    [ "$identifier" = com.typeterrors.gosync.desktop ] || die 'Unexpected application identity.'
    for binary in gosync gosync-desktop; do
        if [ ! -f "$candidate/Contents/MacOS/$binary" ] || [ ! -x "$candidate/Contents/MacOS/$binary" ]; then die "Application is missing $binary."; fi
    done
    codesign --verify --deep --strict "$candidate" || die 'Application signature verification failed.'
    stage=$(mktemp -d "$app_dir/.gosync-install.XXXXXXXX") || die 'Cannot stage application.'
    ditto "$candidate" "$stage/GoSync.app" || die 'Cannot copy the application.'
    check_stopped
    if [ -e "$destination" ]; then mv "$destination" "$stage/previous.app" || die 'Cannot preserve previous application.'; fi
    mv "$stage/GoSync.app" "$destination" || die 'Cannot install application; restoring previous version.'
    committed=true
    say "Installed $destination"
    say 'This build is ad-hoc signed, not Developer ID signed or notarized.'
    say 'If macOS blocks opening it, review System Settings > Privacy & Security > Open Anyway.'
    if [ "$launch" = true ]; then
        open "$destination" || say "Open $destination in Finder to finish setup."
    fi
}

install_ubuntu() {
    need apt-get
    need dpkg-deb
    package=$(dpkg-deb --field "$work/$asset" Package) || die 'Invalid Ubuntu package.'
    package_arch=$(dpkg-deb --field "$work/$asset" Architecture) || die 'Missing package architecture.'
    if [ "$package" != gosync-desktop ] || [ "$package_arch" != "$arch" ]; then die 'Downloaded package identity or architecture is wrong.'; fi
    if [ "$(id -u)" = 0 ]; then
        elevated=''
    else
        need sudo
        say 'Ubuntu will request your normal administrator password if needed.'
        sudo -v || die 'Administrator access is needed to install the Ubuntu package.'
        elevated=sudo
    fi
    # Let apt's unprivileged downloader read the already verified public package.
    chmod 755 "$work"
    chmod 644 "$work/$asset"
    say 'Installing the desktop package and its runtime dependencies...'
    $elevated apt-get update || die 'Could not update Ubuntu package indexes.'
    check_stopped
    $elevated apt-get install -y --no-install-recommends "$work/$asset" || die 'Ubuntu package installation failed.'
    say 'Installed GoSync. Launch it from Applications.'
    say 'On GNOME, enable Ubuntu AppIndicators if the tray icon is hidden.'
    if [ "$launch" = true ]; then
        if [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] && [ -n "${DBUS_SESSION_BUS_ADDRESS:-}" ]; then
            # Detach only the tray. Setup inside the app owns daemon activation.
            nohup gosync-desktop </dev/null >/dev/null 2>&1 &
        else
            say 'No graphical session detected; open GoSync after signing into the desktop.'
        fi
    fi
}

main() {
    set -eu
    repo=https://github.com/TypeTerrors/go_sync
    tag='' app_dir='' download_dir='' work='' stage='' destination=''
    launch=true committed=false github_auth=false
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --version|--app-dir|--download-only)
                option=$1
                if [ "$#" -lt 2 ]; then die "$option needs a value."; fi
                [ -n "$2" ] || die "$option needs a value."
                case "$option" in
                    --version) tag=$2 ;;
                    --app-dir) app_dir=$2 ;;
                    --download-only) download_dir=$2 ;;
                esac
                shift 2 ;;
            --no-launch) launch=false; shift ;;
            --github-auth) github_auth=true; shift ;;
            -h|--help)
                printf '%s\n' 'GoSync desktop installer - macOS 12+ / Ubuntu 24.04+, Intel/AMD and ARM64' \
                    'Usage: bash install.sh [--version TAG] [--no-launch]' \
                    '                       [--app-dir /absolute/mac/directory] [--download-only DIR]' \
                    '                       [--github-auth]' \
                    'Downloads a published desktop release and verifies its SHA-256 checksum.' \
                    '--github-auth uses an authenticated GitHub CLI for a private repository.' \
                    'Existing configuration/history are preserved. Stop GoSync before updating.'
                return ;;
            *) die "Unknown option: $1 (use --help)." ;;
        esac
    done
    [ -z "$tag" ] || valid_tag "$tag" || die 'Invalid release tag. Expected v_<6 hex SHA>.<hex timestamp>.'
    need curl
    if [ "$github_auth" = true ]; then need gh; fi
    need uname
    case "$(uname -s)" in
        Darwin)
            platform=darwin
            need sw_vers
            major=$(sw_vers -productVersion | cut -d. -f1)
            [ "$major" -ge 12 ] || die 'GoSync Desktop requires macOS 12 or newer.'
            arch=$(uname -m)
            # A terminal under Rosetta reports x86_64 on an Apple Silicon Mac.
            if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then arch=arm64; fi
            need shasum
            [ -n "$tag" ] || need osascript
            ;;
        Linux)
            platform=linux
            [ -r /etc/os-release ] || die 'Cannot identify this Linux distribution.'
            # Read data rather than sourcing an OS metadata file as shell code.
            distro=$(sed -n 's/^ID=//p' /etc/os-release | tr -d '"')
            version=$(sed -n 's/^VERSION_ID=//p' /etc/os-release | tr -d '"')
            [ "$distro" = ubuntu ] || die 'This installer supports Ubuntu; use the manual packages for other distributions.'
            case "$version" in [0-9][0-9].[0-9][0-9]) ;; *) die 'Cannot identify the Ubuntu version.' ;; esac
            [ "${version%%.*}" -ge 24 ] || die 'GoSync Desktop packages require Ubuntu 24.04 or newer.'
            need dpkg
            arch=$(dpkg --print-architecture)
            need sha256sum
            [ -n "$tag" ] || need python3
            [ -z "$app_dir" ] || die '--app-dir is for macOS only.'
            ;;
        *) die 'Supported desktops are macOS and Ubuntu.' ;;
    esac
    case "$arch" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die "Unsupported architecture: $arch" ;; esac
    asset="gosync-desktop-$platform-$arch"
    if [ "$platform" = darwin ]; then asset="$asset.zip"; else asset="$asset.deb"; fi
    if [ -z "$download_dir" ]; then
        if [ "$(id -u)" = 0 ] && { [ "$platform" = darwin ] || [ "$launch" = true ]; }; then
            die 'Run as your normal desktop user, without sudo. Ubuntu requests administrator access only for package installation. Root automation can use --no-launch on Ubuntu.'
        fi
        check_stopped
    fi
    work=$(mktemp -d "${TMPDIR:-/tmp}/gosync-install.XXXXXXXX") || die 'Cannot create temporary download directory.'
    trap cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    printf '\nGoSync Desktop\n\n'
    say "Detected $platform / $arch"
    if [ -z "$tag" ]; then
        say 'Finding the newest complete desktop release...'
        if [ "$github_auth" = true ]; then
            gh api 'repos/TypeTerrors/go_sync/releases?per_page=100' > "$work/releases.json" || die 'GitHub access failed. Run gh auth login with an account that can read TypeTerrors/go_sync.'
        else
            fetch -o "$work/releases.json" 'https://api.github.com/repos/TypeTerrors/go_sync/releases?per_page=100' || die 'Cannot read GitHub releases. A private repository requires --github-auth and an authenticated GitHub CLI; see the README.'
        fi
        tag=$(select_release) || die 'No compatible release could be selected. See https://github.com/TypeTerrors/go_sync/releases'
    fi
    valid_tag "$tag" || die 'GitHub returned an unexpected release tag.'
    say "Release $tag"
    say "Downloading ${asset}..."
    if [ "$github_auth" = true ]; then
        gh release download "$tag" --repo TypeTerrors/go_sync --dir "$work" --pattern "$asset" --pattern "$asset.sha256" || die 'Cannot download this desktop package with your GitHub access. Nothing was installed.'
    else
        fetch -o "$work/$asset.sha256" "$repo/releases/download/$tag/$asset.sha256" || die "This release has no checksum for $asset. Nothing was installed."
        fetch -o "$work/$asset" "$repo/releases/download/$tag/$asset" || die 'Package download failed. Nothing was installed.'
    fi
    expected=$(awk -v name="$asset" 'NF == 2 && $2 == name && length($1) == 64 && $1 !~ /[^0-9a-f]/ { hash=$1; count++; next } { bad=1 } END { if (count != 1 || bad) exit 1; print hash }' "$work/$asset.sha256") || die 'Malformed release checksum. Nothing was installed.'
    if [ "$platform" = darwin ]; then actual=$(shasum -a 256 "$work/$asset"); else actual=$(sha256sum "$work/$asset"); fi
    actual=${actual%% *}
    [ "$actual" = "$expected" ] || die 'SHA-256 verification failed. Nothing was installed.'
    say 'SHA-256 verified.'
    if [ -n "$download_dir" ]; then
        mkdir -p "$download_dir"
        if [ -e "$download_dir/$asset" ] || [ -L "$download_dir/$asset" ] || [ -e "$download_dir/$asset.sha256" ] || [ -L "$download_dir/$asset.sha256" ]; then die 'Download destination already contains these files; choose an empty directory.'; fi
        (set -C; cat "$work/$asset" > "$download_dir/$asset"; cat "$work/$asset.sha256" > "$download_dir/$asset.sha256")
        say "Saved verified package and checksum to $download_dir"
        return
    fi
    if [ "$platform" = darwin ]; then install_mac; else install_ubuntu; fi
    printf '\n'
    say 'Next: open GoSync and choose a folder, or select your existing configuration.'
    say "Synchronization starts only through the app's setup/service controls."
    say 'Help: https://github.com/TypeTerrors/go_sync/blob/main/docs/DESKTOP.md'
}

main "$@"
