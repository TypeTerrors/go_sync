//go:build desktop && (linux || darwin)

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

// dashboardData is shared by the native Cocoa and GTK renderers. Neither
// presentation interprets daemon health or invents traffic measurements.
func (a *App) dashboardData(path string, m status.Model, cfg configfile.Description, now time.Time) map[string]any {
	folder, location := "Choose a folder", "Open Settings to get started"
	if cfg.Folder != "" {
		folder, location = filepath.Base(cfg.Folder), filepath.Dir(cfg.Folder)
		if home, err := os.UserHomeDir(); err == nil {
			if location == home {
				location = "~"
			} else if strings.HasPrefix(location, home+string(os.PathSeparator)) {
				location = "~" + strings.TrimPrefix(location, home)
			}
		}
	}
	title := m.Title
	if m.Health == status.Idle {
		title = "Locally idle"
	}
	if m.Health == status.Syncing && m.Transfers > 0 {
		title = "Syncing"
	}
	if path == "" {
		title = "Not configured"
	}
	summary := "Open details to review the current status"
	switch {
	case path == "":
		summary = "Choose a folder in Settings to begin"
	case m.Title == "Running externally":
		summary = "A separate CLI owns this folder's sync process"
	case m.Health == status.Idle:
		summary = "No local transfers · peers may still be catching up"
	case m.Health == status.Syncing && m.Transfers > 0:
		summary = "Moving verified file data between peers"
	case m.Health == status.Syncing:
		summary = "Reconciling changes with your peers"
	case m.Health == status.Health(status.Stopped):
		summary = "Background synchronization is stopped"
	case m.Health == status.Health(status.Starting):
		summary = "Waiting for the background service to start"
	case m.Title == "Waiting for peers":
		summary = "Waiting for a recent successful peer check"
	case !m.Fresh:
		summary = "Live status unavailable · open details to investigate"
	}
	down, up, files, peers, transfers := "—", "—", "—", "—", "—"
	if m.Fresh {
		files, peers, transfers = fmt.Sprint(m.Files), fmt.Sprintf("%d / %d", m.Peers, m.KnownPeers), fmt.Sprint(m.Transfers)
	}
	if m.Fresh && m.Rates.Valid && finiteRate(m.Rates.Receive) && finiteRate(m.Rates.Send) {
		down, up = rate(m.Rates.Receive), rate(m.Rates.Send)
	}
	security := "Mutual TLS"
	if cfg.Insecure {
		security = "Open LAN"
	}
	if path == "" {
		security = "No folder"
	} else if cfg.Folder == "" {
		security = "Unverified"
	}
	return map[string]any{
		"health": m.Health, "title": title, "detail": m.Detail, "summary": summary,
		"folder": folder, "location": location, "fullPath": cfg.Folder,
		"security": security, "receive": down, "send": up,
		"files": files, "peers": peers, "transfers": transfers,
		"now": now.UnixMilli(), "points": a.presentation.history.observe(path, m, now),
		"fresh": m.Fresh,
	}
}
