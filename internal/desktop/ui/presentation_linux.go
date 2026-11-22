//go:build desktop && linux

package ui

import (
	"context"
	"encoding/json"
	"time"

	"fyne.io/systray"
	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

type presentationState struct {
	history   activityHistory
	dashboard *dashboardProcess
}

func (a *App) runPresentation() error {
	a.presentation.dashboard = newDashboardProcess(a.session.Context, a.actions)
	defer a.presentation.dashboard.close()
	// Register before the notifier publishes ItemIsMenu. Some hosts activate the
	// dashboard directly; GNOME also retains the explicit Open Dashboard item.
	systray.SetOnTapped(a.openDashboard)
	systray.Run(a.ready, a.session.Close)
	return nil
}

func (a *App) openDashboard() {
	if err := a.presentation.dashboard.open(); err != nil {
		a.action(func(context.Context) error { return err })
	}
}

func (a *App) preparePresentation() {
	item := systray.AddMenuItem("Open Dashboard", "Folder, live activity and synchronization controls")
	go func() {
		for {
			select {
			case <-a.session.Context.Done():
				return
			case <-item.ClickedCh:
				a.openDashboard()
			}
		}
	}()
	systray.AddSeparator()
}

func (a *App) finishPresentation() {}

func (a *App) present(path string, m status.Model, cfg configfile.Description, service status.Service, now time.Time) {
	data := a.dashboardData(path, m, cfg, now)
	a.fillWindowData(data, path, cfg, m, service, now)
	available := !a.busy.Load()
	data["busy"] = !available
	data["canStart"] = available && path != "" && service.State != status.Unknown && service.State != status.Running && service.State != status.Starting && !m.Fresh
	data["showStart"] = service.State != status.Running && service.State != status.Starting && !m.Fresh
	data["canStop"] = available && service.Installed && (service.State == status.Running || service.State == status.Starting || service.State == status.Failed)
	data["showStop"] = service.Installed && (service.State == status.Running || service.State == status.Starting || service.State == status.Failed)
	data["canRescan"] = available && m.Fresh
	data["login"] = service.Enabled
	data["canLogin"] = available && service.Installed
	data["trayLogin"] = a.native.TrayLogin()
	if encoded, err := json.Marshal(data); err == nil {
		a.presentation.dashboard.update(encoded)
	}
}

func loginItems(enabled bool) (*systray.MenuItem, *systray.MenuItem) {
	menu := systray.AddMenuItem("Launch at Login", "Independent daemon and tray preferences")
	return menu.AddSubMenuItemCheckbox("Start synchronization", "", false),
		menu.AddSubMenuItemCheckbox("Show tray app", "", enabled)
}
