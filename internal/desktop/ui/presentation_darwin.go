//go:build desktop && darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#cgo LDFLAGS: -framework UniformTypeIdentifiers
#include <stdlib.h>
void GSPrepareDashboard(void);
void GSPrepareWindows(void);
void GSUpdateDashboard(const char *json);
*/
import "C"

import (
	"encoding/json"
	"time"
	"unsafe"

	"fyne.io/systray"
	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

type presentationState struct{ history activityHistory }

func (a *App) preparePresentation() {
	windowApp.Store(a)
	C.GSPrepareDashboard()
	C.GSPrepareWindows()
	systray.SetTitle("")
}

func (a *App) finishPresentation() {
	for _, item := range []*systray.MenuItem{a.folder, a.peers, a.files, a.transfers, a.receive, a.send} {
		item.Hide()
	}
}

func loginItems(enabled bool) (*systray.MenuItem, *systray.MenuItem) {
	menu := systray.AddMenuItem("Launch at Login", "Independent background service and menu bar preferences")
	return menu.AddSubMenuItemCheckbox("Start synchronization", "", false),
		menu.AddSubMenuItemCheckbox("Show menu bar app", "", enabled)
}

func (a *App) present(path string, m status.Model, cfg configfile.Description, service status.Service, now time.Time) {
	dashboard := a.dashboardData(path, m, cfg, now)
	a.updateWindows(dashboard, path, cfg, m, service, now)
	data, err := json.Marshal(dashboard)
	if err == nil {
		jsonString := C.CString(string(data))
		C.GSUpdateDashboard(jsonString)
		C.free(unsafe.Pointer(jsonString))
	}
	if service.State == status.Running || service.State == status.Starting || m.Fresh {
		a.start.Hide()
	} else {
		a.start.Show()
	}
	if service.Installed && (service.State == status.Running || service.State == status.Starting || service.State == status.Failed) {
		a.stop.Show()
	} else {
		a.stop.Hide()
	}
}

func (a *App) runPresentation() error {
	defer windowApp.Store(nil)
	systray.Run(a.ready, a.session.Close)
	return nil
}
