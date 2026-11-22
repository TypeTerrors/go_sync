#!/usr/bin/env python3
"""Native GTK widget/graph checks. Run under xvfb-run in CI, or a desktop session."""
import importlib.util
import pathlib
import sys
import unittest

import cairo

sys.dont_write_bytecode = True
path = pathlib.Path(__file__).resolve().parents[1] / "internal/desktop/ui/dashboard_linux.py"
spec = importlib.util.spec_from_file_location("dashboard", path)
dashboard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dashboard)


class DashboardTests(unittest.TestCase):
    def setUp(self):
        self.actions = []
        self.window = dashboard.Dashboard(self.actions.append)
        self.window.hide()

    def tearDown(self):
        self.window.destroy()

    def test_snapshot_and_action_sensitivity(self):
        self.window.update({"title": "Syncing", "health": "syncing", "security": "Mutual TLS",
                            "folder": "Studio <&>", "peers": "2 / 3", "receive": "4.2 MB/s",
                            "canRescan": True, "canStop": True, "showStop": True, "showStart": False,
                            "canLogin": True, "login": True, "trayLogin": True})
        self.assertEqual(self.window.folder.get_text(), "Studio <&>")
        self.assertEqual(self.window.metrics["peers"].get_text(), "2 / 3")
        self.assertEqual(self.window.metrics["receive"].get_text(), "4.2 MB/s")
        self.assertFalse(self.window.controls["start"].get_visible())
        self.assertTrue(self.window.controls["stop"].get_visible())
        self.assertEqual(self.actions, [], "snapshot checkbox updates must not toggle services")
        self.window.controls["stop"].clicked()
        self.window.controls["rescan"].clicked()
        self.assertEqual(self.actions, ["stop", "rescan"])
        self.window.update({"canRescan": False, "busy": True})
        self.window.activate("rescan")
        self.window.activate("settings")
        self.assertEqual(self.actions, ["stop", "rescan"])

    def test_stale_history_and_both_theme_graphs(self):
        data = {"now": 60000, "fresh": True, "points": [
            {"time": 1000, "receive": 4000, "send": 1000, "valid": True},
            {"time": 2000, "receive": 0, "send": 0, "valid": False},
            {"time": 3000, "receive": 2000, "send": 4000, "valid": True}]}
        self.window.update(data)
        self.assertEqual(self.window.graph_caption.get_text(), "Peak 4.0 kB/s · now")
        for dark in (True, False):
            # Change only this test process's GTK settings, not the desktop.
            self.window.appearance = None
            settings = dashboard.Gtk.Settings.get_default()
            settings.set_property("gtk-theme-name", "Adwaita")
            settings.set_property("gtk-application-prefer-dark-theme", dark)
            self.window.theme()
            self.assertEqual(self.window.dark, dark)
            self.assertEqual(self.window.get_style_context().has_class("dark"), dark)
            surface = cairo.ImageSurface(cairo.FORMAT_ARGB32, 400, 60)
            self.window.draw_graph(self.window.graph, cairo.Context(surface))
        data["fresh"] = False
        self.window.update(data)
        self.assertEqual(self.window.graph_caption.get_text(), "Historical · status unavailable")
        self.window.update({"points": []})
        self.assertEqual(self.window.graph_caption.get_text(), "Waiting for measurements")

    def test_close_keeps_tray_and_daemon_independent(self):
        self.window.dismiss()
        self.assertFalse(self.window.get_visible())
        self.assertEqual(self.actions, [])
        self.window.update({"show": True})
        self.assertTrue(self.window.get_visible())
        self.window.activate("quit")
        self.assertEqual(self.actions, ["quit"])

class WindowTests(unittest.TestCase):
    def setUp(self):
        self.actions = []
        self.settings = dashboard.SettingsWindow(self.actions.append)
        self.activity = dashboard.ActivityWindow(self.actions.append)

    def tearDown(self):
        self.settings.dismiss()
        self.settings.destroy()
        self.activity.destroy()

    def drain(self):
        import time
        until = time.monotonic() + .08
        while time.monotonic() < until:
            while dashboard.Gtk.events_pending():
                dashboard.Gtk.main_iteration_do(False)
            time.sleep(.005)

    def choose_directory(self, folder):
        import time
        self.settings.picker.set_filename(folder)
        until = time.monotonic() + 3
        while self.settings.picker.get_filename() != folder and time.monotonic() < until:
            self.drain()
        self.assertEqual(self.settings.picker.get_filename(), folder)

    def test_settings_selection_security_and_independent_login_controls(self):
        data = {"configPath": "/private/secure.yaml", "fullPath": "/files/Studio <&>",
                "statePath": "/history", "security": "Mutual TLS", "title": "Stopped",
                "canChangeFolder": True, "canStart": True, "installed": True,
                "login": True, "trayLogin": False}
        self.settings.update(data)
        self.assertEqual(self.settings.folder.get_text(), "/files/Studio <&>")
        self.assertIn("authenticated and encrypted", self.settings.security.get_text())
        self.assertTrue(self.settings.controls["login"].get_active())
        self.assertFalse(self.settings.controls["tray-login"].get_active())
        self.assertEqual(self.actions, [])
        self.settings.controls["start-stop"].clicked()
        self.settings.controls["tray-login"].set_active(True)
        self.assertEqual(self.actions, ["start", "tray-login"])
        data.update(canStart=False, canStop=True, canChangeFolder=False, title="Syncing")
        self.settings.update(data)
        self.assertFalse(self.settings.controls["choose-folder"].get_sensitive())
        self.assertFalse(self.settings.controls["choose-config"].get_sensitive())
        self.settings.controls["start-stop"].clicked()
        self.assertEqual(self.actions[-1], "stop")
        data["busy"] = True
        self.settings.update(data)
        count = len(self.actions)
        self.settings.activate("remove-service")
        self.settings.pick(True)
        self.assertEqual(len(self.actions), count)
        self.assertIsNone(self.settings.picker)
        self.settings.activate("quit")
        self.assertEqual(self.actions[-1], "quit", "busy service actions must not prevent tray exit")

    def test_picker_cancellation_and_service_change_do_not_select(self):
        self.settings.update({"canChangeFolder": True})
        self.settings.pick(True)
        self.assertEqual(self.settings.picker.get_action(), dashboard.Gtk.FileChooserAction.SELECT_FOLDER)
        self.settings.picker.response(dashboard.Gtk.ResponseType.CANCEL)
        self.assertIsNone(self.settings.picker)
        self.assertEqual(self.actions, [])
        self.settings.pick(False)
        self.assertEqual(self.settings.picker.get_action(), dashboard.Gtk.FileChooserAction.OPEN)
        self.assertEqual(self.settings.picker.get_filter().get_name(), "GoSync configuration (YAML / JSON)")
        self.settings.dismiss()
        self.assertIsNone(self.settings.picker)
        self.assertEqual(self.actions, [])

    def test_folder_picker_emits_literal_local_path_only_after_acceptance(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="gosync GTK folder ") as folder:
            self.settings.update({"canChangeFolder": True})
            self.settings.pick(True)
            self.choose_directory(folder)
            self.settings.picker.response(dashboard.Gtk.ResponseType.ACCEPT)
            self.assertEqual(self.actions, ["choose-folder:" + folder])
            self.settings.pick(True)
            self.choose_directory(folder)
            self.settings.update({"canChangeFolder": False, "busy": True})
            self.settings.picker.response(dashboard.Gtk.ResponseType.ACCEPT)
            self.assertEqual(len(self.actions), 1, "service started while picker was open")

    def test_live_tables_full_details_and_selection_survive_refresh(self):
        data = {"configPath": "/secure.yaml", "fullPath": "/Studio", "fresh": True,
                "heartbeat": "12:34:56", "title": "Syncing", "health": "syncing",
                "canRescan": True, "uploads": 2, "downloads": 1,
                "peerRows": [{"name": "localhost:55001", "state": "Recently reachable", "time": "12:34", "detail": "Node: peer-identity"}],
                "transferRows": [{"name": "long/<literal>& file.bin", "received": "1 MiB", "target": "40 MiB", "time": "12:34", "detail": "Payload received, not a completion percentage."}],
                "issueRows": [{"name": "link", "detail": "Unsupported symlink"}]}
        self.activity.update(data)
        for key in ("peers", "transfers", "attention"):
            self.activity.stack.set_visible_child_name(key)
            page = self.activity.pages[key]
            self.assertEqual(len(page.store), 1)
            page.table.get_selection().select_path(dashboard.Gtk.TreePath.new_first())
            data["heartbeat"] = "12:34:57"
            self.activity.update(data)
            self.assertIsNotNone(page.table.get_selection().get_selected()[1])
            buffer = page.details.get_buffer()
            self.assertIn(data[{"peers": "peerRows", "transfers": "transferRows", "attention": "issueRows"}[key]][0]["detail"],
                          buffer.get_text(buffer.get_start_iter(), buffer.get_end_iter(), True))
        self.activity.stack.set_visible_child_name("transfers")
        self.assertIn("2 sending", self.activity.footnote.get_text())
        self.assertIn("Upload file names are not included", self.activity.footnote.get_text())
        self.activity.controls["rescan"].clicked()
        self.activity.controls["settings"].clicked()
        self.assertEqual(self.actions, ["rescan", "settings"])

    def test_null_empty_starting_error_and_historical_snapshots(self):
        data = {"title": None, "points": None, "peerRows": None, "transferRows": None, "issueRows": None}
        self.settings.update(data)
        self.activity.update(data)
        self.assertFalse(self.settings.controls["start-stop"].get_sensitive())
        self.assertEqual(self.activity.metrics["receive"].get_text(), "—")
        self.assertIn("Choose a folder", self.activity.pages["peers"].empty.get_text())
        for health, expected in [("starting", "Starting"), ("stopped", "Start synchronization"), ("error", "unavailable"), ("unavailable", "unavailable")]:
            self.activity.update({"configPath": "/profile", "health": health})
            self.assertIn(expected, self.activity.pages["peers"].empty.get_text())
        self.activity.update({"configPath": "/profile", "heartbeat": "yesterday", "fresh": False,
                              "peerRows": [{"name": "peer", "state": "Historical"}],
                              "now": 60000, "points": [{"valid": True, "receive": 1200, "send": 0, "time": 59000}]})
        self.assertIn("Historical", self.activity.freshness.get_text())
        self.assertIn("historical", self.activity.graph_caption.get_text())
        self.assertIn("last-known", self.activity.footnote.get_text())
        self.assertIn("Fresh measurements are unavailable", self.activity.footnote.get_text())
        self.assertFalse(self.activity.controls["rescan"].get_sensitive())
        self.assertEqual(len(self.activity.pages["peers"].store), 1)

    def test_window_close_reopen_never_emits_stop_or_quit(self):
        windows = dashboard.Windows(self.actions.append)
        try:
            for name in ("settings", "details"):
                windows.update({"showWindow": name})
                window = windows.windows[name]
                window.dismiss()
                self.assertFalse(window.get_visible())
                windows.update({"title": "Stopped", "points": None})
                windows.update({"showWindow": name})
                self.assertIs(windows.windows[name], window)
                self.assertTrue(window.get_visible())
            self.assertEqual(self.actions, [])
            self.assertNotIn("dashboard", windows.windows, "opening Settings must not flash a dashboard")
        finally:
            for window in windows.windows.values():
                window.destroy()

    def test_both_native_windows_render_in_light_and_dark(self):
        import hashlib
        images = []
        for dark in (False, True):
            settings = dashboard.Gtk.Settings.get_default()
            settings.set_property("gtk-theme-name", "Adwaita")
            settings.set_property("gtk-application-prefer-dark-theme", dark)
            for window in (self.settings, self.activity):
                window.appearance = None
                window.theme()
                self.assertEqual(window.get_style_context().has_class("dark"), dark)
                window.update({"title": "Stopped", "configPath": "/a very long path/" * 20, "fullPath": "/Studio", "points": None})
                self.drain()
                width, height = window.get_size()
                self.assertLessEqual(width, 1000, "long paths expanded the window")
                surface = cairo.ImageSurface(cairo.FORMAT_ARGB32, width, height)
                window.draw(cairo.Context(surface))
                images.append(hashlib.sha256(bytes(surface.get_data())).hexdigest())
        self.assertNotEqual(images[:2], images[2:])


if __name__ == "__main__":
    unittest.main()
