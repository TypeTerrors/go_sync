# Desktop implementation choices

[`fyne.io/systray` v1.12.2](https://github.com/fyne-io/systray) provides native Cocoa
menus and Go D-Bus StatusNotifier/AppIndicator support without the Fyne framework.
Its upstream was active in October 2026 when selected. It is BSD-3-Clause licensed.

The original [`getlantern/systray`](https://github.com/getlantern/systray) also
supports Cocoa, but its Linux backend requires GTK/AppIndicator development
libraries. Its latest observed push was July 2024. The maintained Fyne fork
reduces native Linux dependencies. macOS still requires CGO and the Apple SDK;
both CPU architectures build on native runners. Legacy XEmbed-only trays are
outside the target. The D-Bus dependency is
[`godbus/dbus`](https://github.com/godbus/dbus), BSD-2-Clause licensed.

[`ncruces/zenity` v0.10.15](https://github.com/ncruces/zenity), MIT licensed, supplies
small native dialogs using Zenity on Ubuntu and system dialogs on macOS. No
browser, Electron runtime, or HTTP control listener is introduced. The linked
upstream repositories contain each dependency's license and source.

Ubuntu's rich dashboard uses GTK 3 through the distribution's Python/GObject
bindings (`python3-gi`, `python3-gi-cairo`, `gir1.2-gtk-3.0`). The small renderer is
embedded in the Go executable and starts on demand using isolated system Python;
no pip installation or GTK development headers are needed. It exchanges bounded,
local pipe messages with Go: display snapshots in, allowlisted actions out. Go
owns the status/history model and dispatches the same service handlers as the tray
menu. Closing its window hides the dashboard; quitting the tray terminates its
renderer without stopping the daemon. No browser, web server, or desktop shell
extension specific to GoSync is needed.

GNOME AppIndicator menus cannot contain arbitrary GTK views, so the graph lives
in a compact native dashboard launched from the tray. Hosts that support
StatusNotifier activation open it directly; **Open Dashboard** remains available
in the standard tray menu. The macOS Cocoa menu retains its embedded dashboard.
Both renderers share the same Go presentation and activity-history logic.

The daemon remains authoritative for config parsing, TLS, initialization, Badger
locking, rescans, and synchronization. `gosync config --config FILE` adds a
read-only description through its existing parser/validation without opening
history. Shared telemetry DTOs preserve the JSON fields and add a PID for matching
OS-service heartbeats. The tray does not import/link the synchronization engine.

Desktop code separates config discovery/client, status/rate/display models,
lifecycle controller, OS adapters, and native UI. OS commands use argument arrays,
not shell interpolation. GUI code uses the `desktop` build tag and platform
constraints. Service managers own the daemon; canceling the UI has no Stop call.
