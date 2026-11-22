//go:build desktop && (linux || darwin)

// Package ui is the native tray adapter. All I/O and dialogs run away from the
// native main loop; menu mutations use systray's goroutine-safe API.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"github.com/TypeTerrors/go_sync/assets/icons"
	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/config"
	"github.com/TypeTerrors/go_sync/internal/desktop/controller"
	"github.com/TypeTerrors/go_sync/internal/desktop/platform"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"github.com/ncruces/zenity"
)

type App struct {
	store                                                  config.Store
	client                                                 config.Client
	native                                                 *platform.Native
	control                                                *controller.Controller
	session                                                *controller.Session
	mu                                                     sync.RWMutex
	path                                                   string
	cfg                                                    configfile.Description
	problem                                                string
	model                                                  status.Model
	service                                                status.Service
	busy                                                   atomic.Bool
	starting                                               atomic.Bool
	presentation                                           presentationState
	actions                                                chan string
	health, folder, peers, files, transfers, receive, send *systray.MenuItem
	start, stop, rescan, login, trayLogin                  *systray.MenuItem
}

func Run(binary, executable string) error {
	store, err := config.DefaultStore()
	if err != nil {
		return err
	}
	lock, err := platform.LockTray(filepath.Join(store.Dir, "desktop.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if !zenity.IsAvailable() {
		return fmt.Errorf("native dialogs are unavailable; on Ubuntu install zenity and launch from your desktop session")
	}
	native, err := platform.New(binary, executable)
	if err != nil {
		return err
	}
	a := &App{store: store, client: config.Client{Binary: binary, Run: platform.Exec{}}, native: native, session: controller.NewSession(), actions: make(chan string, 16)}
	a.control = &controller.Controller{Service: native, Daemon: a.client, Read: status.Read, Busy: platform.StateBusy}
	defer a.session.Close()
	return a.runPresentation()
}
func (a *App) options(title string) []zenity.Option {
	return []zenity.Option{zenity.Title("GoSync · " + title), zenity.Context(a.session.Context)}
}
func (a *App) question(title, text string) error {
	if handled, err := a.nativeQuestion(title, text); handled {
		return err
	}
	return zenity.Question(text, append(a.options(title), zenity.DefaultCancel())...)
}
func (a *App) action(fn func(context.Context) error) {
	if !a.busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busy.Store(false)
		ctx, cancel := context.WithTimeout(a.session.Context, 2*time.Minute)
		defer cancel()
		if err := fn(ctx); err != nil && !errors.Is(err, zenity.ErrCanceled) && a.session.Context.Err() == nil {
			_ = a.showMessage("Could not complete action", err.Error(), true)
		}
	}()
}
func (a *App) ready() {
	systray.SetTemplateIcon(icons.Template("unavailable"), icons.Color("unavailable"))
	systray.SetTooltip("GoSync Desktop")
	systray.SetTitle("GoSync")
	a.preparePresentation()
	a.health = systray.AddMenuItem("GoSync · Loading…", "")
	a.health.Disable()
	a.folder = systray.AddMenuItem("Folder: not configured", "")
	a.folder.Disable()
	a.peers = systray.AddMenuItem("Peers: —", "")
	a.peers.Disable()
	a.files = systray.AddMenuItem("Tracked entries: —", "")
	a.files.Disable()
	a.transfers = systray.AddMenuItem("Transfers: —", "")
	a.transfers.Disable()
	a.receive = systray.AddMenuItem("↓ Receiving: —", "")
	a.receive.Disable()
	a.send = systray.AddMenuItem("↑ Sending: —", "")
	a.send.Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("Open Sync Folder", "")
	a.rescan = systray.AddMenuItem("Rescan Now", "")
	details := systray.AddMenuItem("Status, Peers & Transfers…", "")
	systray.AddSeparator()
	a.start = systray.AddMenuItem("Start GoSync", "")
	a.stop = systray.AddMenuItem("Stop GoSync", "")
	settings := systray.AddMenuItem("Settings…", "")
	logs := systray.AddMenuItem("View Logs…", "")
	systray.AddSeparator()
	a.login, a.trayLogin = loginItems(a.native.TrayLogin())
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Tray · keep syncing", "")
	a.finishPresentation()
	go a.poll()
	go func() {
		for {
			select {
			case <-a.session.Context.Done():
				return
			case <-quit.ClickedCh:
				a.session.Close()
				systray.Quit()
				return
			case <-open.ClickedCh:
				a.dispatch("open")
			case <-a.rescan.ClickedCh:
				a.dispatch("rescan")
			case <-a.start.ClickedCh:
				a.dispatch("start")
			case <-a.stop.ClickedCh:
				a.dispatch("stop")
			case <-details.ClickedCh:
				a.dispatch("details")
			case <-logs.ClickedCh:
				a.dispatch("logs")
			case <-settings.ClickedCh:
				a.dispatch("settings")
			case <-a.login.ClickedCh:
				a.dispatch("login")
			case <-a.trayLogin.ClickedCh:
				a.dispatch("tray-login")
			case action := <-a.actions:
				a.dispatch(action)
			}
		}
	}()
	a.action(func(ctx context.Context) error {
		path, err := a.store.Discover()
		if err != nil {
			a.setProblem(err)
			return err
		}
		if path == "" {
			return a.setup(ctx)
		}
		a.mu.Lock()
		a.path = path
		a.mu.Unlock()
		if err = a.load(ctx, path); err != nil {
			a.setProblem(err)
			return err
		}
		state, err := a.native.Inspect(ctx)
		if err != nil {
			return err
		}
		if !state.Installed {
			if a.handlePresentationAction("settings") {
				return nil
			}
			return a.adopt(ctx)
		}
		return nil
	})
}
func (a *App) selection() (string, configfile.Description) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.path, a.cfg
}
func (a *App) setProblem(err error) { a.mu.Lock(); defer a.mu.Unlock(); a.problem = err.Error() }
func (a *App) load(ctx context.Context, path string) error {
	cfg, err := a.client.Resolve(ctx, path)
	if err != nil {
		return err
	}
	if err = a.store.Select(path); err != nil {
		return err
	}
	a.mu.Lock()
	a.path, a.cfg, a.problem = path, cfg, ""
	a.mu.Unlock()
	return nil
}

const disclosure = "Automatic LAN mode is unencrypted and unauthenticated. Other GoSync peers in the same LAN group can read, modify, and delete files in this folder. Use only on a trusted network.\n\nExisting files will be published to peers; remote changes and deletions can arrive here. Concurrent edits may create conflicts. Keep a separate backup."

func (a *App) setup(ctx context.Context) error {
	if a.handlePresentationAction("settings") {
		return nil
	}
	choice, err := zenity.List("Choose how to set up GoSync. Existing configurations and history are preserved.", []string{"Choose a folder for automatic LAN sync", "Use an existing YAML or JSON configuration"}, a.options("Welcome")...)
	if err != nil {
		return err
	}
	if choice == "" {
		return zenity.ErrCanceled
	}
	if choice == "Use an existing YAML or JSON configuration" {
		return a.chooseConfig(ctx)
	}
	folder, err := zenity.SelectFile(append(a.options("Choose synchronization folder"), zenity.Directory())...)
	if err != nil {
		return err
	}
	if err = a.question("Enable LAN synchronization?", "Folder: "+folder+"\n\n"+disclosure+"\n\nContinue to initialize this folder, start synchronization, and enable it at login?"); err != nil {
		return err
	}
	// Once approved, let initialization finish even if the user closes the UI.
	// Killing init midway could leave a newly created configuration incomplete.
	initialize, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if err = a.client.Init(initialize, folder); err != nil {
		return err
	}
	path, err := configfile.DefaultPath()
	if err != nil {
		return err
	}
	if err = a.load(ctx, path); err != nil {
		return err
	}
	if err = a.startService(ctx, path); err != nil {
		return err
	}
	return a.native.Enable(ctx, true)
}
func (a *App) adopt(ctx context.Context) error {
	path, cfg := a.selection()
	text := "Use the existing configuration at:\n" + path + "\n\nFolder: " + cfg.Folder + "\nHistory: " + cfg.StateDir + "\n\nIts configuration, identity, and history will be preserved."
	if cfg.Insecure {
		text += "\n\n" + disclosure
	} else {
		text += "\n\nThis configuration uses mutual TLS. Its certificates and group will be preserved."
	}
	if err := a.question("Enable background synchronization?", text+"\n\nStart synchronization and enable it at login?"); err != nil {
		return err
	}
	if err := a.startService(ctx, path); err != nil {
		return err
	}
	return a.native.Enable(ctx, true)
}
func (a *App) startService(ctx context.Context, path string) error {
	a.starting.Store(true)
	defer a.starting.Store(false)
	deadline, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return a.control.Start(deadline, path)
}
func (a *App) startSync(ctx context.Context) error {
	path, cfg := a.selection()
	if path == "" {
		return a.setup(ctx)
	}
	if err := a.load(ctx, path); err != nil {
		return err
	}
	_, cfg = a.selection()
	if cfg.Insecure {
		if err := a.question("Start automatic LAN sync?", disclosure); err != nil {
			return err
		}
	}
	return a.startService(ctx, path)
}
func (a *App) chooseConfig(ctx context.Context) error {
	s, err := a.native.Inspect(ctx)
	if err != nil {
		return err
	}
	if s.State == status.Running || s.State == status.Starting {
		return fmt.Errorf("stop the managed service before selecting another configuration")
	}
	path, err := zenity.SelectFile(append(a.options("Choose existing configuration"), zenity.FileFilters{{Name: "GoSync configuration", Patterns: []string{"*.yaml", "*.yml", "*.json"}}})...)
	if err != nil {
		return err
	}
	if err = a.load(ctx, path); err != nil {
		return err
	}
	return a.adopt(ctx)
}
func (a *App) settings(ctx context.Context) error {
	path, cfg := a.selection()
	security := "Mutual TLS"
	if cfg.Insecure {
		security = "Unauthenticated, unencrypted LAN"
	}
	choice, err := zenity.List("Configuration: "+path+"\nFolder: "+cfg.Folder+"\nState: "+cfg.StateDir+"\nSecurity: "+security+"\n\nStop synchronization before editing paths. Never delete history to reset a peer.", []string{"Open configuration folder", "Open configuration in editor", "Use an existing configuration", "Set up a new folder", "Remove desktop service and login integration"}, a.options("Settings")...)
	if err != nil {
		return err
	}
	switch choice {
	case "Open configuration folder":
		return a.native.Open(ctx, a.store.Dir)
	case "Open configuration in editor":
		if path == "" {
			return fmt.Errorf("no configuration selected")
		}
		return a.native.Open(ctx, path)
	case "Use an existing configuration":
		return a.chooseConfig(ctx)
	case "Set up a new folder":
		if existing, err := a.store.Discover(); err != nil || existing != "" {
			return fmt.Errorf("an existing configuration is preserved; select a separately initialized configuration to change folders")
		}
		return a.setup(ctx)
	case "Remove desktop service and login integration":
		if err = a.question("Remove background service?", "This stops the managed daemon and removes its desktop service and login integration. Synchronized files, configuration, and history remain intact. The tray will quit afterward."); err != nil {
			return err
		}
		if err = a.native.Uninstall(ctx); err != nil {
			return err
		}
		a.session.Close()
		systray.Quit()
	}
	return nil
}
func (a *App) poll() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var meter status.Meter
	previousPath := ""
	previousHealth := status.Health("")
	for {
		path, cfg := a.selection()
		if path != previousPath {
			meter.Reset()
			previousPath = path
		}
		ctx, cancel := context.WithTimeout(a.session.Context, 3*time.Second)
		service, serviceErr := a.native.Inspect(ctx)
		cancel()
		s, readErr := status.Read(cfg.StateDir)
		if cfg.StateDir == "" {
			readErr = fmt.Errorf("choose a synchronization folder in Settings")
		}
		if serviceErr != nil {
			service.Detail = serviceErr.Error()
			service.State = status.Unknown
		}
		now := time.Now()
		if a.starting.Load() && !status.Fresh(s, now) {
			service.State = status.Starting
		}
		model := status.Classify(service, s, readErr, now, cfg.SyncInterval, meter.Observe(s, now))
		if path == "" {
			model.Health, model.Title = status.Unavailable, "Choose a folder in Settings"
		}
		a.mu.Lock()
		if a.problem != "" {
			model.Health, model.Title, model.Detail = status.Error, "Configuration error", a.problem
		}
		a.model, a.service = model, service
		a.mu.Unlock()
		if a.session.Context.Err() != nil {
			return
		}
		a.health.SetTitle("GoSync · " + model.Title)
		a.folder.SetTitle("Folder: " + cfg.Folder)
		a.peers.SetTitle(fmt.Sprintf("Peers: %d recently reachable / %d known", model.Peers, model.KnownPeers))
		files := "—"
		if model.Fresh {
			files = fmt.Sprint(model.Files)
		}
		a.files.SetTitle("Tracked entries: " + files)
		a.transfers.SetTitle(fmt.Sprintf("Transfers: %d active", model.Transfers))
		down, up := "—", "—"
		if model.Rates.Valid {
			down, up = rate(model.Rates.Receive), rate(model.Rates.Send)
		}
		a.receive.SetTitle("↓ Receiving: " + down)
		a.send.SetTitle("↑ Sending: " + up)
		if model.Health != previousHealth {
			systray.SetTemplateIcon(icons.Template(string(model.Health)), icons.Color(string(model.Health)))
			previousHealth = model.Health
		}
		if service.Enabled {
			a.login.Check()
		} else {
			a.login.Uncheck()
		}
		if a.native.TrayLogin() {
			a.trayLogin.Check()
		} else {
			a.trayLogin.Uncheck()
		}
		available := !a.busy.Load()
		setEnabled(a.start, available && service.State != status.Running && service.State != status.Starting && !model.Fresh)
		setEnabled(a.stop, available && service.Installed && (service.State == status.Running || service.State == status.Starting || service.State == status.Failed))
		setEnabled(a.rescan, available && model.Fresh)
		a.present(path, model, cfg, service, now)
		select {
		case <-a.session.Context.Done():
			return
		case <-ticker.C:
		}
	}
}
func setEnabled(item *systray.MenuItem, enabled bool) {
	if enabled {
		item.Enable()
	} else {
		item.Disable()
	}
}
func rate(n float64) string {
	if n >= 1e6 {
		return fmt.Sprintf("%.1f MB/s", n/1e6)
	}
	if n >= 1e3 {
		return fmt.Sprintf("%.1f kB/s", n/1e3)
	}
	return fmt.Sprintf("%.0f B/s", n)
}

// ReportStartupError is graphical even when launched without a terminal.
func ReportStartupError(err error) {
	_ = zenity.Error(err.Error(), zenity.Title("GoSync Desktop"))
	fmt.Fprintln(os.Stderr, err)
}

// dispatch is shared by native menus and the Linux dashboard.
func (a *App) dispatch(action string) {
	if a.handlePresentationAction(action) {
		return
	}
	switch action {
	case "quit":
		a.session.Close()
		systray.Quit()
	case "open":
		a.action(func(ctx context.Context) error {
			_, cfg := a.selection()
			if cfg.Folder == "" {
				return fmt.Errorf("choose a folder in Settings first")
			}
			return a.native.Open(ctx, cfg.Folder)
		})
	case "rescan":
		a.action(func(ctx context.Context) error {
			p, _ := a.selection()
			if err := a.control.Rescan(ctx, p); err != nil {
				return err
			}
			return a.showMessage("Rescan", "Local rescan completed. Peer reconciliation continues in the background.", false)
		})
	case "start":
		a.action(a.startSync)
	case "stop":
		a.action(func(ctx context.Context) error { _, cfg := a.selection(); return a.control.Stop(ctx, cfg) })
	case "details":
		a.action(func(ctx context.Context) error {
			a.mu.RLock()
			text := a.model.Details()
			a.mu.RUnlock()
			return a.native.ShowText(ctx, "Status, peers & transfers", text)
		})
	case "logs":
		a.action(a.native.ViewLogs)
	case "settings":
		a.action(a.settings)
	case "login":
		a.action(func(ctx context.Context) error {
			s, err := a.native.Inspect(ctx)
			if err != nil {
				return err
			}
			if !s.Installed {
				return fmt.Errorf("start GoSync once to install the service")
			}
			return a.native.Enable(ctx, !s.Enabled)
		})
	case "tray-login":
		a.action(func(ctx context.Context) error { return a.native.SetTrayLogin(ctx, !a.native.TrayLogin()) })
	}
}
