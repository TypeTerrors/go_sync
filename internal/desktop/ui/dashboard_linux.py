"""Native GTK presentation only. Go owns status, history and all service actions.

Embedded in the tray binary and started on demand with isolated system Python.
No network listener, filesystem configuration reader, or shell execution.
"""
import json
import sys
import threading

import gi
gi.require_version("Gtk", "3.0")
gi.require_version("Gdk", "3.0")
from gi.repository import Gdk, Gio, GLib, Gtk, Pango


STYLE = b"""
window.gosync { background: #fafafa; color: #222222; border-radius: 14px; border: 1px solid #bababa; }
window.gosync.dark { background: #161616; color: #eeeeee; border-color: #565656; }
.gosync label { color: inherit; }
.gosync .muted { color: #686868; }
.gosync.dark .muted { color: #a0a0a0; }
.gosync .title { font-size: 20px; font-weight: 700; }
.gosync .health { font-size: 12px; font-weight: 600; }
.gosync .folder { background: #eeeeee; border-radius: 9px; padding: 10px 12px; }
.gosync.dark .folder { background: #272727; }
.gosync .folder-name { font-size: 14px; font-weight: 600; }
.gosync .small { font-size: 10px; }
.gosync .metric { font-size: 21px; font-feature-settings: "tnum"; }
.gosync .rate { font-size: 20px; font-feature-settings: "tnum"; }
.gosync .caption { font-size: 9px; font-weight: 600; }
.gosync .receive, .gosync .idle, .gosync .syncing { color: #08735b; }
.gosync.dark .receive, .gosync.dark .idle, .gosync.dark .syncing { color: #66e0bd; }
.gosync .send { color: #6e52b3; }
.gosync.dark .send { color: #b8abf5; }
.gosync .warning { color: #a05a00; }
.gosync.dark .warning { color: #ffbd66; }
.gosync .error { color: #c22939; }
.gosync.dark .error { color: #ff8491; }
.gosync button { background: transparent; background-image: none; box-shadow: none;
 border: 0; border-radius: 6px; color: inherit; padding: 6px 8px; min-height: 20px; }
.gosync button:hover, .gosync button:focus { background: alpha(#808080, 0.18); }
.gosync button:disabled { opacity: 0.4; }
.gosync separator { background: alpha(#808080, 0.25); min-height: 1px; margin: 6px 0; }
.gosync expander { padding: 4px 8px; }
.gosync .window-title { font-size: 24px; font-weight: 700; }
.gosync .section-title { font-size: 16px; font-weight: 600; }
.gosync .card { background: #eeeeee; border-radius: 10px; padding: 16px; border: 1px solid #d3d3d3; }
.gosync.dark .card { background: #222222; border-color: #464646; }
.gosync .path { color: #345d8b; }
.gosync.dark .path { color: #a8c6e8; }
.gosync .action-button { background: alpha(#808080, 0.12); border: 1px solid alpha(#808080, 0.15); padding: 7px 12px; }
.gosync .action-button:hover { background: alpha(#808080, 0.24); }
.gosync .row-detail, .gosync .row-detail text { background: transparent; color: #686868; font-size: 11px; }
.gosync.dark .row-detail, .gosync.dark .row-detail text { color: #b4b4b4; }
.gosync treeview { background: #ffffff; color: #222222; }
.gosync.dark treeview { background: #202020; color: #eeeeee; }
.gosync treeview:selected { background: #246c60; color: #ffffff; }
.gosync stackswitcher button:checked { background: #246c60; color: #ffffff; }

"""


def styled(widget, *names):
    for name in names:
        widget.get_style_context().add_class(name)
    return widget


def label(text="", *classes):
    widget = styled(Gtk.Label(label=text, xalign=0), *classes)
    widget.set_ellipsize(Pango.EllipsizeMode.END)
    return widget


def rate(value):
    if value >= 1e6:
        return f"{value / 1e6:.1f} MB/s"
    if value >= 1e3:
        return f"{value / 1e3:.1f} kB/s"
    return f"{value:.0f} B/s"


class Dashboard(Gtk.Window):
    def __init__(self, emit):
        super().__init__(title="GoSync Dashboard")
        self.emit = emit
        self.data = {}
        self.dark = False
        self.updating = False
        self.controls = {}
        self.set_wmclass("gosync-desktop", "GoSync")
        self.set_icon_name("folder-sync")
        self.set_decorated(False)
        self.set_resizable(False)
        self.set_skip_taskbar_hint(True)
        self.set_type_hint(Gdk.WindowTypeHint.UTILITY)
        self.set_position(Gtk.WindowPosition.MOUSE)
        self.set_default_size(360, -1)
        self.connect("delete-event", lambda *_: self.dismiss())
        self.connect("key-press-event", self.key)
        styled(self, "gosync")
        self.get_accessible().set_name("GoSync Dashboard")
        provider = Gtk.CssProvider()
        provider.load_from_data(STYLE)
        Gtk.StyleContext.add_provider_for_screen(self.get_screen(), provider, Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
        scroller = Gtk.ScrolledWindow()
        scroller.set_policy(Gtk.PolicyType.NEVER, Gtk.PolicyType.AUTOMATIC)
        scroller.set_propagate_natural_height(True)
        monitor = self.get_display().get_primary_monitor() or self.get_display().get_monitor(0)
        scroller.set_max_content_height(max(300, monitor.get_workarea().height - 80))
        self.add(scroller)
        root = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0, margin=16)
        scroller.add(root)

        header = Gtk.Box(spacing=12)
        mark = styled(Gtk.Image.new_from_icon_name("view-refresh-symbolic", Gtk.IconSize.LARGE_TOOLBAR), "receive")
        header.pack_start(mark, False, False, 0)
        name = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=3)
        name.pack_start(label("GoSync", "title"), False, False, 0)
        self.health = label("Loading…", "health", "muted")
        self.health.set_max_width_chars(25)
        name.pack_start(self.health, False, False, 0)
        header.pack_start(name, True, True, 0)
        self.security = label("", "muted", "small")
        header.pack_start(self.security, False, False, 0)
        close = Gtk.Button.new_from_icon_name("window-close-symbolic", Gtk.IconSize.MENU)
        close.set_tooltip_text("Close dashboard · keep syncing")
        close.get_accessible().set_name("Close dashboard")
        close.connect("clicked", lambda *_: self.dismiss())
        header.pack_start(close, False, False, 0)
        root.pack_start(header, False, False, 0)

        folder = styled(Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4), "folder")
        folder.set_margin_top(16)
        folder.set_margin_bottom(13)
        self.folder = label("Choose a folder", "folder-name")
        self.folder.set_max_width_chars(38)
        self.folder.get_accessible().set_name("Synchronization folder")
        self.location = label("Open Settings to get started", "muted", "small")
        self.location.set_ellipsize(Pango.EllipsizeMode.MIDDLE)
        self.location.set_max_width_chars(42)
        folder.pack_start(self.folder, False, False, 0)
        folder.pack_start(self.location, False, False, 0)
        root.pack_start(folder, False, False, 0)

        self.metrics = {}
        metrics = Gtk.Box(homogeneous=True, spacing=12)
        for key, caption, description in [
            ("peers", "RECENT PEERS", "Recently reachable peers out of known peers"),
            ("files", "ENTRIES", "Tracked entries including directories"),
            ("transfers", "TRANSFERS", "Active transfers")]:
            column = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4)
            value = label("—", "metric")
            value.get_accessible().set_name(description)
            value.set_tooltip_text(description)
            column.pack_start(value, False, False, 0)
            column.pack_start(label(caption, "caption", "muted"), False, False, 0)
            metrics.pack_start(column, True, True, 0)
            self.metrics[key] = value
        root.pack_start(metrics, False, False, 0)
        root.pack_start(Gtk.Separator(), False, False, 5)

        traffic = Gtk.Box(homogeneous=True, spacing=12)
        for key, caption in [("receive", "↓  RECEIVE"), ("send", "↑  SEND")]:
            column = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4)
            column.pack_start(label(caption, "caption", key), False, False, 0)
            value = label("—", "rate")
            value.get_accessible().set_name(f"{key.capitalize()} payload per second")
            column.pack_start(value, False, False, 0)
            traffic.pack_start(column, True, True, 0)
            self.metrics[key] = value
        root.pack_start(traffic, False, False, 0)
        self.graph = Gtk.DrawingArea()
        self.graph.set_size_request(-1, 54)
        self.graph.set_margin_top(12)
        self.graph.connect("draw", self.draw_graph)
        self.graph.get_accessible().set_name("60-second activity: receive solid teal, send dashed purple; missing samples leave gaps")
        root.pack_start(self.graph, False, False, 0)
        captions = Gtk.Box(spacing=8)
        captions.pack_start(label("60 seconds ago", "small", "muted"), False, False, 0)
        self.graph_caption = label("Waiting for measurements", "small", "muted")
        self.graph_caption.set_xalign(1)
        captions.pack_end(self.graph_caption, True, True, 0)
        root.pack_start(captions, False, False, 5)
        self.summary = label("", "small", "muted")
        self.summary.set_max_width_chars(48)
        root.pack_start(self.summary, False, False, 8)
        root.pack_start(Gtk.Separator(), False, False, 0)

        for key, title, icon, shortcut in [
            ("open", "Open Sync Folder", "folder-symbolic", "Ctrl O"),
            ("rescan", "Rescan Now", "view-refresh-symbolic", "Ctrl R"),
            ("details", "Status, Peers & Transfers…", "utilities-system-monitor-symbolic", ""),
            (None, "", "", ""),
            ("start", "Start GoSync", "media-playback-start-symbolic", ""),
            ("stop", "Stop GoSync", "media-playback-stop-symbolic", ""),
            ("settings", "Settings…", "preferences-system-symbolic", "Ctrl ,"),
            ("logs", "View Logs…", "text-x-generic-symbolic", "")]:
            if key is None:
                root.pack_start(Gtk.Separator(), False, False, 0)
                continue
            button = Gtk.Button()
            row = Gtk.Box(spacing=10)
            row.pack_start(Gtk.Image.new_from_icon_name(icon, Gtk.IconSize.MENU), False, False, 0)
            row.pack_start(label(title), True, True, 0)
            row.pack_end(label(shortcut, "small", "muted"), False, False, 0)
            button.add(row)
            button.get_accessible().set_name(title)
            button.connect("clicked", lambda _, action=key: self.activate(action))
            root.pack_start(button, False, False, 0)
            self.controls[key] = button
        root.pack_start(Gtk.Separator(), False, False, 0)
        login = Gtk.Expander(label="Launch at Login")
        login_options = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=4, margin=8)
        for key, title in [("login", "Start synchronization"), ("tray-login", "Show tray app")]:
            check = Gtk.CheckButton(label=title)
            check.connect("toggled", lambda _, action=key: self.activate(action))
            login_options.pack_start(check, False, False, 0)
            self.controls[key] = check
        login.add(login_options)
        root.pack_start(login, False, False, 0)
        root.pack_start(Gtk.Separator(), False, False, 0)
        quit_button = Gtk.Button(label="Quit Tray · keep syncing")
        quit_button.connect("clicked", lambda *_: self.activate("quit"))
        root.pack_start(quit_button, False, False, 0)

        settings = Gtk.Settings.get_default()
        settings.connect("notify::gtk-theme-name", self.theme)
        settings.connect("notify::gtk-application-prefer-dark-theme", self.theme)
        schema = Gio.SettingsSchemaSource.get_default()
        self.appearance = None
        if schema and schema.lookup("org.gnome.desktop.interface", True):
            self.appearance = Gio.Settings.new("org.gnome.desktop.interface")
            if "color-scheme" in self.appearance.props.settings_schema.list_keys():
                self.appearance.connect("changed::color-scheme", self.theme)
        self.theme()
        self.show_all()
        self.controls["stop"].hide()

    def theme(self, *_):
        settings = Gtk.Settings.get_default()
        self.dark = settings.get_property("gtk-application-prefer-dark-theme") or "dark" in settings.get_property("gtk-theme-name").lower()
        if self.appearance and "color-scheme" in self.appearance.props.settings_schema.list_keys():
            self.dark |= self.appearance.get_string("color-scheme") == "prefer-dark"
        style = self.get_style_context()
        (style.add_class if self.dark else style.remove_class)("dark")
        self.graph.queue_draw()

    def dismiss(self):
        self.hide()
        return True

    def key(self, _, event):
        if event.keyval == Gdk.KEY_Escape:
            return self.dismiss()
        if event.state & Gdk.ModifierType.CONTROL_MASK:
            action = {Gdk.KEY_o: "open", Gdk.KEY_r: "rescan", Gdk.KEY_comma: "settings", Gdk.KEY_q: "quit"}.get(event.keyval)
            if action:
                self.activate(action)
                return True
        return False

    def activate(self, action):
        if self.updating:
            return
        control = self.controls.get(action)
        if control and not control.get_sensitive():
            return
        self.emit(action)
        if action in ("open", "settings", "details", "logs"):
            self.hide()

    def update(self, data):
        if data.get("show"):
            self.present()
            return
        data = {key: value for key, value in data.items() if value is not None}
        self.data = data
        self.updating = True
        self.health.set_text(data.get("title", "Status unavailable"))
        style = self.health.get_style_context()
        for name in ("idle", "syncing", "warning", "error"):
            style.remove_class(name)
        if data.get("health") in ("idle", "syncing", "warning", "error"):
            style.add_class(data["health"])
        self.health.set_tooltip_text(data.get("detail", ""))
        self.security.set_text(data.get("security", ""))
        self.security.set_tooltip_text("Unauthenticated, unencrypted LAN" if data.get("security") == "Open LAN" else data.get("security", ""))
        self.folder.set_text(data.get("folder", "Choose a folder"))
        self.location.set_text(data.get("location", ""))
        for widget in (self.folder, self.location):
            widget.set_tooltip_text(data.get("fullPath", ""))
        for key, widget in self.metrics.items():
            widget.set_text(data.get(key, "—"))
        self.summary.set_text(data.get("summary", ""))
        self.summary.set_tooltip_text(data.get("detail") or data.get("summary", ""))
        points = data.get("points") or []
        measured = any(p["valid"] for p in points)
        peak = max((max(p["receive"], p["send"]) for p in points if p["valid"]), default=0)
        caption = "Waiting for measurements" if not measured else "Historical · status unavailable" if not data.get("fresh") else f"Peak {rate(peak)} · now"
        self.graph_caption.set_text(caption)
        self.graph.set_tooltip_text(caption)
        for key, field in [("start", "canStart"), ("stop", "canStop"), ("rescan", "canRescan"), ("login", "canLogin")]:
            self.controls[key].set_sensitive(data.get(field, False))
        self.controls["start"].set_visible(data.get("showStart", True))
        self.controls["stop"].set_visible(data.get("showStop", False))
        for key in ("open", "details", "logs", "settings", "tray-login"):
            self.controls[key].set_sensitive(not data.get("busy", False))
        self.controls["login"].set_active(data.get("login", False))
        self.controls["tray-login"].set_active(data.get("trayLogin", False))
        self.updating = False
        self.graph.queue_draw()

    def draw_graph(self, widget, cr):
        return draw_activity(widget, cr, self.data, self.dark)


def draw_activity(widget, cr, data, dark):
    width, height = widget.get_allocated_width(), widget.get_allocated_height()
    cr.set_source_rgba(0.5, 0.5, 0.5, 0.3)
    cr.set_line_width(0.5)
    for row in range(3):
        y = 2 + row * (height - 4) / 2
        cr.move_to(0, y)
        cr.line_to(width, y)
    for column in range(5):
        x = column * width / 4
        cr.move_to(x, 2)
        cr.line_to(x, height - 2)
    cr.stroke()
    points = data.get("points") or []
    peak = max(1, max((max(p["receive"], p["send"]) for p in points if p["valid"]), default=0))
    now = data.get("now", 0)
    colors = [(0.40, 0.88, 0.74), (0.72, 0.67, 0.96)] if dark else [(0.04, 0.43, 0.34), (0.43, 0.32, 0.70)]
    for key, color, dash in [("receive", colors[0], []), ("send", colors[1], [3, 3])]:
        cr.set_source_rgba(*color, 1 if data.get("fresh") else 0.45)
        cr.set_line_width(1.75)
        cr.set_dash(dash)
        last = None
        for point in points:
            if not point["valid"]:
                last = None
                continue
            x = min(1, max(0, (point["time"] - (now - 60000)) / 60000)) * width
            y = 2 + (height - 4) * (1 - point[key] / peak)
            if last is not None and point["time"] - last <= 5000:
                cr.line_to(x, y)
            else:
                cr.move_to(x, y)
            last = point["time"]
        cr.stroke()
    return False

def text(data, key, default=""):
    value = data.get(key)
    return value if isinstance(value, str) else default


def paragraph(value="", *classes):
    widget = label(value, *classes)
    widget.set_ellipsize(Pango.EllipsizeMode.NONE)
    widget.set_line_wrap(True)
    widget.set_line_wrap_mode(Pango.WrapMode.WORD_CHAR)
    widget.set_width_chars(1)
    widget.set_hexpand(True)
    widget.set_selectable(True)
    return widget


def install_style(window):
    provider = Gtk.CssProvider()
    provider.load_from_data(STYLE)
    Gtk.StyleContext.add_provider_for_screen(window.get_screen(), provider, Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)


def watch_theme(window):
    settings = Gtk.Settings.get_default()
    connections = [(settings, settings.connect(name, window.theme)) for name in
                   ("notify::gtk-theme-name", "notify::gtk-application-prefer-dark-theme")]
    schema = Gio.SettingsSchemaSource.get_default()
    window.appearance = None
    if schema and schema.lookup("org.gnome.desktop.interface", True):
        window.appearance = Gio.Settings.new("org.gnome.desktop.interface")
        if "color-scheme" in window.appearance.props.settings_schema.list_keys():
            connections.append((window.appearance, window.appearance.connect("changed::color-scheme", window.theme)))
    window.connect("destroy", lambda *_: [obj.disconnect(handler) for obj, handler in connections])
    window.theme()


def theme(window):
    settings = Gtk.Settings.get_default()
    window.dark = bool(settings.get_property("gtk-application-prefer-dark-theme")) or "dark" in settings.get_property("gtk-theme-name").lower()
    if window.appearance and "color-scheme" in window.appearance.props.settings_schema.list_keys():
        window.dark |= window.appearance.get_string("color-scheme") == "prefer-dark"
    style = window.get_style_context()
    (style.add_class if window.dark else style.remove_class)("dark")
    window.queue_draw()


def health_style(widget, health):
    style = widget.get_style_context()
    for name in ("idle", "syncing", "warning", "error"):
        style.remove_class(name)
    if health in ("idle", "syncing", "warning", "error"):
        style.add_class(health)


def health_icon(widget, data):
    names = {"idle": "emblem-ok-symbolic", "syncing": "view-refresh-symbolic",
             "warning": "dialog-warning-symbolic", "error": "dialog-error-symbolic",
             "stopped": "media-playback-stop-symbolic", "starting": "content-loading-symbolic"}
    widget.set_from_icon_name(names.get(data.get("health"), "dialog-question-symbolic"), Gtk.IconSize.LARGE_TOOLBAR)
    health_style(widget, data.get("health"))
    widget.set_tooltip_text(text(data, "title", "Status unavailable"))


class DesktopWindow(Gtk.Window):
    def __init__(self, title, emit, width, height):
        super().__init__(title=title)
        self.emit, self.data, self.controls, self.updating, self.dark = emit, {}, {}, False, False
        self.picker = None
        self.set_wmclass("gosync-desktop", "GoSync")
        self.set_icon_name("folder-sync")
        self.set_default_size(width, height)
        self.set_position(Gtk.WindowPosition.CENTER)
        self.get_accessible().set_name(title)
        styled(self, "gosync", "desktop-window")
        header = Gtk.HeaderBar(title=title, show_close_button=True)
        self.set_titlebar(header)
        self.connect("delete-event", lambda *_: self.dismiss())
        self.connect("key-press-event", self.key)
        install_style(self)
        scroller = Gtk.ScrolledWindow()
        scroller.set_policy(Gtk.PolicyType.NEVER, Gtk.PolicyType.AUTOMATIC)
        self.add(scroller)
        self.root = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=16, margin=24)
        scroller.add(self.root)
        watch_theme(self)

    def theme(self, *_):
        theme(self)

    def dismiss(self):
        if self.picker:
            self.picker.response(Gtk.ResponseType.CANCEL)
        self.hide()
        return True

    def key(self, _, event):
        if event.keyval == Gdk.KEY_Escape:
            return self.dismiss()
        if event.state & Gdk.ModifierType.CONTROL_MASK:
            if event.keyval == Gdk.KEY_w:
                return self.dismiss()
            action = {Gdk.KEY_comma: "settings", Gdk.KEY_1: "details", Gdk.KEY_q: "quit", Gdk.KEY_r: "rescan"}.get(event.keyval)
            if action:
                self.activate(action)
                return True
        return False

    def activate(self, action):
        if self.updating or (self.data.get("busy") and action not in ("quit", "settings", "details")):
            return
        control = self.controls.get(action)
        if control is not None and not control.get_sensitive():
            return
        if action == "rescan" and not self.data.get("canRescan"):
            return
        self.emit(action)

    def button(self, title, action, callback=None):
        button = styled(Gtk.Button(label=title), "action-button")
        button.get_accessible().set_name(title)
        button.connect("clicked", callback or (lambda *_: self.activate(action)))
        self.controls[action] = button
        return button

    def card(self):
        card = styled(Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=12), "card")
        self.root.pack_start(card, False, False, 0)
        return card


class SettingsWindow(DesktopWindow):
    def __init__(self, emit):
        super().__init__("GoSync Settings", emit, 660, 680)
        header = Gtk.Box(spacing=16)
        header.pack_start(label("Settings", "window-title"), True, True, 0)
        header.pack_end(self.button("View Activity", "details"), False, False, 0)
        self.root.pack_start(header, False, False, 0)
        self.root.pack_start(paragraph("Choose what syncs and when GoSync runs.", "muted"), False, False, 0)
        folder = self.card()
        row = Gtk.Box(spacing=12)
        row.pack_start(Gtk.Image.new_from_icon_name("folder-symbolic", Gtk.IconSize.LARGE_TOOLBAR), False, False, 0)
        row.pack_start(label("Synchronization folder", "section-title"), True, True, 0)
        row.pack_end(self.button("Choose Folder…", "choose-folder", lambda *_: self.pick(True)), False, False, 0)
        folder.pack_start(row, False, False, 0)
        self.folder = paragraph("No folder selected", "path")
        self.folder.get_accessible().set_name("Selected synchronization folder")
        folder.pack_start(self.folder, False, False, 0)
        self.folder_note = paragraph("Previous folders, configurations, and sync history stay intact.", "muted", "small")
        folder.pack_start(self.folder_note, False, False, 0)
        service = self.card()
        row = Gtk.Box(spacing=12)
        row.pack_start(label("Synchronization", "section-title"), True, True, 0)
        row.pack_end(self.button("Start", "start-stop", lambda *_: self.activate("stop" if self.data.get("canStop") else "start")), False, False, 0)
        service.pack_start(row, False, False, 0)
        self.health = paragraph("Not configured", "health")
        service.pack_start(self.health, False, False, 0)
        for key, title in [("login", "Start synchronization at login"), ("tray-login", "Show GoSync in the tray at login")]:
            check = Gtk.CheckButton(label=title)
            check.connect("toggled", lambda _, action=key: self.activate(action))
            self.controls[key] = check
            service.pack_start(check, False, False, 0)
        self.security = paragraph("Connection mode appears after selecting a folder.", "muted", "small")
        service.pack_start(self.security, False, False, 0)
        existing = self.card()
        row = Gtk.Box(spacing=12)
        row.pack_start(label("Existing configuration", "section-title"), True, True, 0)
        row.pack_end(self.button("Use Existing…", "choose-config", lambda *_: self.pick(False)), False, False, 0)
        existing.pack_start(row, False, False, 0)
        self.config = paragraph("Import a GoSync YAML or JSON file", "path")
        existing.pack_start(self.config, False, False, 0)
        row = Gtk.Box(spacing=12)
        row.pack_start(self.button("Open Config Folder", "config-folder"), False, False, 0)
        row.pack_start(paragraph("Imports preserve your identity and sync history.", "small", "muted"), True, True, 0)
        existing.pack_start(row, False, False, 0)
        footer = Gtk.Box(spacing=14)
        footer.pack_start(styled(self.button("Remove Service…", "remove-service"), "error"), False, False, 0)
        self.feedback = paragraph("Closing this window keeps GoSync running.", "small", "muted")
        footer.pack_end(self.feedback, True, True, 0)
        self.root.pack_start(footer, False, False, 0)
        self.show_all()
        self.update({})

    def pick(self, folder):
        if not self.data.get("canChangeFolder") or self.data.get("busy"):
            return
        if self.picker:
            self.picker.present()
            return
        picker = Gtk.FileChooserDialog(title="Choose a synchronization folder" if folder else "Use an existing GoSync configuration",
                                       transient_for=self, modal=True,
                                       action=Gtk.FileChooserAction.SELECT_FOLDER if folder else Gtk.FileChooserAction.OPEN)
        picker.add_buttons("Cancel", Gtk.ResponseType.CANCEL, "Choose Folder" if folder else "Use Configuration", Gtk.ResponseType.ACCEPT)
        picker.set_default_response(Gtk.ResponseType.CANCEL)
        picker.set_select_multiple(False)
        picker.set_local_only(True)
        if not folder:
            filters = Gtk.FileFilter()
            filters.set_name("GoSync configuration (YAML / JSON)")
            for pattern in ("*.yaml", "*.yml", "*.json"):
                filters.add_pattern(pattern)
            picker.add_filter(filters)
        self.picker = picker

        def selected(dialog, response):
            path = dialog.get_filename() if response == Gtk.ResponseType.ACCEPT else None
            self.picker = None
            dialog.destroy()
            # Recheck a changing snapshot; Go also checks service/state ownership.
            if path and self.data.get("canChangeFolder") and not self.data.get("busy"):
                self.emit(("choose-folder:" if folder else "choose-config:") + path)
        picker.connect("response", selected)
        picker.show()

    def update(self, data):
        self.data = data
        self.updating = True
        busy = bool(data.get("busy"))
        selected = bool(text(data, "configPath"))
        can_change = bool(data.get("canChangeFolder")) and not busy
        self.folder.set_text(text(data, "fullPath", "No folder selected") or "No folder selected")
        self.folder_note.set_text("Stop synchronization before choosing another folder." if not can_change and selected else "Previous folders, configurations, and sync history stay intact.")
        self.health.set_text(text(data, "title", "Not configured"))
        health_style(self.health, data.get("health"))
        security = text(data, "security")
        mode = "Mutual TLS · authenticated and encrypted" if security == "Mutual TLS" else "Open LAN · unauthenticated and unencrypted" if security == "Open LAN" else "Configuration needs attention before starting."
        self.security.set_text(mode if selected else "Connection mode appears after selecting a folder.")
        self.security.set_tooltip_text("Folder group: " + text(data, "group"))
        self.config.set_text(text(data, "configPath") or "Import a GoSync YAML or JSON file")
        self.config.set_tooltip_text("Sync history: " + text(data, "statePath"))
        for key in ("choose-folder", "choose-config"):
            self.controls[key].set_sensitive(can_change)
        self.controls["choose-folder"].set_label("Choose New Folder…" if selected else "Choose Folder…")
        self.controls["choose-folder"].get_accessible().set_name(self.controls["choose-folder"].get_label())
        stop = bool(data.get("canStop"))
        self.controls["start-stop"].set_label("Stop" if stop else "Start")
        self.controls["start-stop"].get_accessible().set_name("Stop" if stop else "Start")
        self.controls["start-stop"].set_sensitive(not busy and (stop or bool(data.get("canStart"))))
        self.controls["login"].set_sensitive(bool(data.get("installed")) and not busy)
        self.controls["tray-login"].set_sensitive(not busy)
        self.controls["login"].set_active(bool(data.get("login")))
        self.controls["tray-login"].set_active(bool(data.get("trayLogin")))
        self.controls["config-folder"].set_sensitive(selected and not busy)
        self.controls["remove-service"].set_sensitive(not busy and bool(data.get("installed") or data.get("trayLogin")))
        self.feedback.set_text("Applying changes…" if busy else "Closing this window keeps GoSync running.")
        self.updating = False


class ActivityPage(Gtk.Box):
    def __init__(self, columns, hint):
        super().__init__(orientation=Gtk.Orientation.VERTICAL, spacing=10)
        self.rows, self.hint = [], hint
        self.keys = [key for key, title, width in columns]
        self.store = Gtk.ListStore(*([str] * len(columns)))
        self.table = Gtk.TreeView(model=self.store, headers_visible=True)
        self.table.get_accessible().set_name(columns[0][1] + " list")
        for index, (key, title, width) in enumerate(columns):
            renderer = Gtk.CellRendererText()
            renderer.set_property("ellipsize", Pango.EllipsizeMode.MIDDLE if key == "name" else Pango.EllipsizeMode.END)
            renderer.set_property("ypad", 8)
            column = Gtk.TreeViewColumn(title, renderer, text=index)
            column.set_sizing(Gtk.TreeViewColumnSizing.FIXED)
            column.set_fixed_width(width)
            column.set_resizable(True)
            column.set_expand(index == len(columns)-1)
            self.table.append_column(column)
        self.table.get_selection().connect("changed", self.selection_changed)
        scroller = Gtk.ScrolledWindow()
        scroller.set_policy(Gtk.PolicyType.AUTOMATIC, Gtk.PolicyType.AUTOMATIC)
        scroller.set_min_content_height(180)
        scroller.set_shadow_type(Gtk.ShadowType.IN)
        scroller.add(self.table)
        overlay = Gtk.Overlay()
        overlay.add(scroller)
        self.empty = paragraph("", "muted")
        self.empty.set_halign(Gtk.Align.CENTER)
        self.empty.set_valign(Gtk.Align.CENTER)
        self.empty.set_margin_start(20)
        self.empty.set_margin_end(20)
        self.empty.set_selectable(False)
        overlay.add_overlay(self.empty)
        overlay.set_overlay_pass_through(self.empty, True)
        self.pack_start(overlay, True, True, 0)
        self.details = Gtk.TextView(editable=False, cursor_visible=False, wrap_mode=Gtk.WrapMode.WORD_CHAR)
        self.details.get_accessible().set_name("Selected row details")
        styled(self.details, "row-detail")
        detail_scroll = Gtk.ScrolledWindow()
        detail_scroll.set_policy(Gtk.PolicyType.NEVER, Gtk.PolicyType.AUTOMATIC)
        detail_scroll.set_min_content_height(70)
        detail_scroll.add(self.details)
        self.pack_start(detail_scroll, False, False, 0)
        self.selection_changed()

    def selection_changed(self, *_):
        model, selected = self.table.get_selection().get_selected()
        index = model.get_path(selected).get_indices()[0] if selected is not None else -1
        value = self.hint
        if 0 <= index < len(self.rows):
            row = self.rows[index]
            detail, name = text(row, "detail"), text(row, "name")
            value = detail if detail.startswith(name) else name + "\n" + detail
        self.details.get_buffer().set_text(value)

    def update(self, rows, empty):
        rows = [row for row in (rows or []) if isinstance(row, dict)]
        if rows != self.rows:
            model, selected = self.table.get_selection().get_selected()
            name = model.get_value(selected, 0) if selected is not None else None
            self.table.get_selection().unselect_all()
            self.rows = rows
            self.store.clear()
            for index, row in enumerate(rows):
                self.store.append([text(row, key) for key in self.keys])
                if name is not None and text(row, "name") == name:
                    self.table.get_selection().select_path(Gtk.TreePath.new_from_indices([index]))
        self.empty.set_text(empty)
        self.empty.set_visible(not rows)
        self.selection_changed()


class ActivityWindow(DesktopWindow):
    def __init__(self, emit):
        super().__init__("GoSync Activity", emit, 900, 760)
        header = Gtk.Box(spacing=14)
        self.icon = Gtk.Image()
        header.pack_start(self.icon, False, False, 0)
        self.title_label = label("Activity", "window-title")
        header.pack_start(self.title_label, True, True, 0)
        header.pack_end(self.button("Rescan Now", "rescan"), False, False, 0)
        header.pack_end(self.button("Settings", "settings"), False, False, 0)
        self.root.pack_start(header, False, False, 0)
        self.folder = paragraph("Choose a folder in Settings to get started.", "muted")
        self.root.pack_start(self.folder, False, False, 0)
        metrics = Gtk.Box(spacing=14, homogeneous=True)
        self.metrics = {}
        for key, caption in (("peers", "RECENT PEERS"), ("receive", "↓  RECEIVE"), ("send", "↑  SEND")):
            card = styled(Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=7), "card")
            card.pack_start(label(caption, "caption", key if key != "peers" else "muted"), False, False, 0)
            self.metrics[key] = label("—", "metric")
            card.pack_start(self.metrics[key], False, False, 0)
            metrics.pack_start(card, True, True, 0)
        self.root.pack_start(metrics, False, False, 0)
        chart = self.card()
        chart.pack_start(label("Transfer activity · last 60 seconds", "small", "muted"), False, False, 0)
        self.graph = Gtk.DrawingArea()
        self.graph.set_size_request(-1, 65)
        self.graph.get_accessible().set_name("60-second measured payload activity")
        self.graph.connect("draw", lambda widget, cr: draw_activity(widget, cr, self.data, self.dark))
        chart.pack_start(self.graph, False, False, 0)
        self.graph_caption = paragraph("Waiting for measurements", "small", "muted")
        chart.pack_start(self.graph_caption, False, False, 0)
        self.stack = Gtk.Stack(transition_type=Gtk.StackTransitionType.NONE)
        self.pages = {}
        definitions = [
            ("peers", "Peers", [("name", "Peer", 310), ("state", "Status", 210), ("time", "Last successful sync", 220)],
             "Recently reachable means a successful reconciliation, not a live connection. Select a peer for its identity and any reported error."),
            ("transfers", "Transfers", [("name", "Receiving file", 300), ("received", "Payload received", 155), ("target", "Target size", 125), ("time", "Started", 160)],
             "Payload received counts only bytes received in this attempt, not reused blocks. It is not a completion percentage. Uploads are counted, but per-file upload names are not reported by the service."),
            ("attention", "Attention", [("name", "Item", 280), ("detail", "Needs attention", 460)],
             "Conflicts, scan failures, unsupported paths, configuration errors, and peer errors appear here. Select an item to read its full explanation.")]
        for key, title, columns, hint in definitions:
            self.pages[key] = ActivityPage(columns, hint)
            self.stack.add_titled(self.pages[key], key, title)
        tabs = Gtk.Box(spacing=12)
        switcher = Gtk.StackSwitcher(stack=self.stack)
        tabs.pack_start(switcher, False, False, 0)
        self.freshness = label("No live snapshot", "small", "muted")
        self.freshness.set_xalign(1)
        tabs.pack_end(self.freshness, True, True, 0)
        self.root.pack_start(tabs, False, False, 0)
        self.root.pack_start(self.stack, True, True, 0)
        self.footnote = paragraph("Locally idle does not mean every peer has finished syncing.", "small", "muted")
        self.root.pack_start(self.footnote, False, False, 0)
        self.stack.connect("notify::visible-child-name", lambda *_: self.update_footer())
        self.show_all()
        self.update({})

    def update_footer(self):
        data = self.data
        historical = text(data, "heartbeat") not in ("", "Not yet")
        value = "Locally idle does not mean every peer has finished syncing."
        if not data.get("fresh") and historical:
            value = "These are last-known values. " + ("Start synchronization to receive fresh measurements." if data.get("canStart") else "Fresh measurements are unavailable; check Attention for details.")
        elif self.stack.get_visible_child_name() == "transfers" and data.get("fresh"):
            value = f"{data.get('downloads') or 0} receiving · {data.get('uploads') or 0} sending · Upload file names are not included in the snapshot."
        self.footnote.set_text(value)

    def update(self, data):
        self.data = data
        self.title_label.set_text(text(data, "title", "Activity"))
        self.folder.set_text(text(data, "fullPath") or "Choose a folder in Settings to get started.")
        health_icon(self.icon, data)
        for key, widget in self.metrics.items():
            widget.set_text(text(data, key, "—"))
        self.controls["rescan"].set_sensitive(bool(data.get("canRescan")) and not data.get("busy"))
        heartbeat = text(data, "heartbeat")
        historical = heartbeat not in ("", "Not yet")
        self.freshness.set_text("Live · " + heartbeat if data.get("fresh") else "Historical · " + heartbeat if historical else "No live snapshot")
        points = data.get("points") or []
        measured = [p for p in points if p.get("valid")]
        peak = max((max(p["receive"], p["send"]) for p in measured), default=0)
        caption = f"Receive: solid teal · Send: dashed purple     Peak {rate(peak)}" if measured else "Waiting for measurements"
        if measured and not data.get("fresh"):
            caption += " · historical"
        self.graph_caption.set_text(caption)
        self.graph.queue_draw()
        for key, field, empty in [("peers", "peerRows", "No peers discovered yet"),
                                  ("transfers", "transferRows", "No receiving files in this snapshot"),
                                  ("attention", "issueRows", "No issues reported in this snapshot")]:
            if not text(data, "configPath"):
                empty = "Choose a folder in Settings to see activity here."
            elif not data.get("fresh") and not historical:
                empty = "Starting synchronization…" if data.get("health") == "starting" else "Status is unavailable. Check Attention for details." if data.get("health") in ("error", "unavailable") else "Start synchronization to see live activity."
            self.pages[key].update(data.get(field), empty)
        self.update_footer()


class Windows:
    def __init__(self, emit):
        self.emit, self.data, self.windows = emit, {}, {}

    def update(self, data):
        if not isinstance(data, dict):
            return
        if data.get("showWindow") or data.get("show"):
            self.show(data.get("showWindow", "dashboard"))
            return
        self.data = data
        for window in self.windows.values():
            window.update(data)

    def show(self, name):
        classes = {"dashboard": Dashboard, "settings": SettingsWindow, "details": ActivityWindow}
        if name not in classes:
            return
        if name not in self.windows:
            self.windows[name] = classes[name](self.emit)
        window = self.windows[name]
        window.update(self.data)
        window.present()


def main():
    windows = Windows(lambda action: print(json.dumps({"action": action}), flush=True))
    # Coalesce snapshots even while a native menu/dialog is busy. Only GTK's main
    # thread touches widgets; EOF closes this renderer, never the daemon.
    lock = threading.Lock()
    pending = {}
    queued = False

    def drain():
        nonlocal queued
        with lock:
            data, shows = pending.pop("data", None), pending.pop("shows", [])
            queued = False
        if data is not None:
            windows.update(data)
        for name in shows:
            windows.show(name)
        return False

    def read():
        nonlocal queued
        try:
            for line in sys.stdin:
                data = json.loads(line)
                with lock:
                    if not isinstance(data, dict):
                        continue
                    if data.get("showWindow") or data.get("show"):
                        name = data.get("showWindow", "dashboard")
                        if name in ("dashboard", "settings", "details"):
                            shows = pending.setdefault("shows", [])
                            if name not in shows:
                                shows.append(name)
                    else:
                        pending["data"] = data
                    if not queued:
                        queued = True
                        GLib.idle_add(drain)
        finally:
            GLib.idle_add(Gtk.main_quit)

    threading.Thread(target=read, daemon=True).start()
    Gtk.main()


if __name__ == "__main__":
    main()
