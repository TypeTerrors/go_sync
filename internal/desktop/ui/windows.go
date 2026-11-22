//go:build desktop && (linux || darwin)

package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fyne.io/systray"
	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/platform"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

// Window actions share the same configuration and lifecycle rules on both OSes.
func (a *App) handleWindowAction(action string) bool {
	switch action {
	case "config-folder":
		a.action(func(ctx context.Context) error {
			path, _ := a.selection()
			if path == "" {
				return a.native.Open(ctx, a.store.Dir)
			}
			return a.native.Open(ctx, filepath.Dir(path))
		})
		return true
	case "remove-service":
		a.action(func(ctx context.Context) error {
			if err := a.question("Remove background service?", "Synchronization will stop and desktop login integration will be removed. Your folders, configuration, and sync history will stay intact. The tray will quit."); err != nil {
				return err
			}
			if err := a.native.Uninstall(ctx); err != nil {
				return err
			}
			a.session.Close()
			systray.Quit()
			return nil
		})
		return true
	}
	if folder, ok := strings.CutPrefix(action, "choose-folder:"); ok {
		a.action(func(ctx context.Context) error { return a.createFolderProfile(ctx, folder) })
		return true
	}
	if path, ok := strings.CutPrefix(action, "choose-config:"); ok {
		a.action(func(ctx context.Context) error {
			if err := a.canChangeFolder(ctx); err != nil {
				return err
			}
			cfg, err := a.client.Resolve(ctx, path)
			if err != nil {
				return err
			}
			if busy, err := platform.StateBusy(cfg.StateDir); err != nil {
				return err
			} else if busy {
				return fmt.Errorf("this folder is already syncing in another process; stop it before switching")
			}
			// Selecting is deliberately separate from starting synchronization.
			return a.load(ctx, path)
		})
		return true
	}
	return false
}

func (a *App) canChangeFolder(ctx context.Context) error {
	s, err := a.native.Inspect(ctx)
	if err != nil {
		return err
	}
	if s.State == status.Unknown {
		return fmt.Errorf("service status is unavailable; try again after its status is known")
	}
	if s.State == status.Running || s.State == status.Starting {
		return fmt.Errorf("stop synchronization before choosing another folder")
	}
	_, cfg := a.selection()
	if cfg.StateDir != "" {
		busy, err := platform.StateBusy(cfg.StateDir)
		if err != nil {
			return err
		}
		if busy {
			return fmt.Errorf("another process is syncing this folder; stop it before switching")
		}
	}
	return nil
}

func (a *App) createFolderProfile(ctx context.Context, folder string) error {
	if err := a.canChangeFolder(ctx); err != nil {
		return err
	}
	folder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return err
	}
	info, err := os.Stat(folder)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(folder) || !info.IsDir() {
		return fmt.Errorf("choose an existing folder")
	}
	// Every known profile participates, including imported configurations. Reusing
	// a folder must preserve its identity and state, not silently create new history.
	profiles, err := a.store.Profiles()
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		cfg, err := a.client.Resolve(ctx, profile)
		if err != nil {
			return fmt.Errorf("cannot check previous configuration %s; restore it or use an existing configuration: %w", profile, err)
		}
		root, err := filepath.EvalSymlinks(cfg.Folder)
		if err != nil {
			return fmt.Errorf("cannot verify previous folder %s: %w", cfg.Folder, err)
		}
		if root != folder {
			if foldersOverlap(root, folder) {
				return fmt.Errorf("this folder overlaps an existing synchronized folder; use its existing configuration and history")
			}
			continue
		}
		busy, err := platform.StateBusy(cfg.StateDir)
		if err != nil {
			return err
		}
		if busy {
			return fmt.Errorf("another process is syncing that folder; stop it before switching")
		}
		return a.load(ctx, profile)
	}
	if err := a.question("Set up this folder?", "Folder: "+folder+"\n\n"+disclosure+"\n\nA separate configuration and history will be created. Previous folders stay intact. Synchronization stays stopped until you click Start."); err != nil {
		return err
	}
	base := filepath.Join(a.store.Dir, "folders")
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "workspace-")
	if err != nil {
		return err
	}
	// JSON preserves literal paths, including environment-placeholder characters.
	path := filepath.Join(dir, "config.json")
	initialize, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if err := a.client.InitAt(initialize, folder, path); err != nil {
		return err
	}
	return a.load(initialize, path)
}

func foldersOverlap(a, b string) bool {
	within := func(parent, child string) bool {
		r, err := filepath.Rel(parent, child)
		return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
	}
	return within(a, b) || within(b, a)
}

func snapshotTime(t time.Time) string {
	if t.IsZero() {
		return "Not yet"
	}
	return t.Local().Format("Jan 2, 15:04:05")
}

func byteSize(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// Detail rows come from the same atomic snapshot as the tray. Stale data stays
// explicitly historical; payload counters are never presented as percentages.
func windowData(path string, cfg configfile.Description, m status.Model, service status.Service, now time.Time) map[string]any {
	peers, transfers, issues := []map[string]string{}, []map[string]string{}, []map[string]string{}
	window := max(30*time.Second, 3*cfg.SyncInterval)
	for _, p := range m.Snapshot.Peers {
		state := "Waiting"
		age := now.Sub(p.LastSuccess)
		switch {
		case !m.Fresh:
			state = "Historical"
		case p.LastError != "":
			state = "Needs attention"
		case p.Pending:
			state = "Reconciling"
		case !p.LastSuccess.IsZero() && age >= -time.Second && age <= window:
			state = "Recently reachable"
		}
		detail := "Node: " + p.NodeID
		if p.NodeID == "" {
			detail = "Node identity not yet reported"
		}
		if p.LastError != "" {
			detail += "\n" + p.LastError
			issues = append(issues, map[string]string{"name": p.Address, "detail": p.LastError})
		}
		peers = append(peers, map[string]string{"name": p.Address, "state": state, "time": snapshotTime(p.LastSuccess), "detail": detail})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i]["name"] < peers[j]["name"] })
	for _, t := range m.Snapshot.Transfers {
		transfers = append(transfers, map[string]string{"name": t.Path, "received": byteSize(t.ReceivedBytes), "target": byteSize(t.TargetBytes), "time": snapshotTime(t.Started), "detail": fmt.Sprintf("%s\nPayload received this attempt: %d bytes. Target file size: %d bytes. Reused blocks are not included, so this is not a completion percentage.", t.Path, t.ReceivedBytes, t.TargetBytes)})
	}
	sort.Slice(transfers, func(i, j int) bool { return transfers[i]["name"] < transfers[j]["name"] })
	if m.Snapshot.ScanError != "" {
		issues = append(issues, map[string]string{"name": "Folder scan", "detail": m.Snapshot.ScanError})
	}
	if path != "" && m.Detail != "" && m.Detail != m.Snapshot.ScanError &&
		(m.Health == status.Error || m.Health == status.Unavailable || m.Title == "Running externally") {
		issues = append(issues, map[string]string{"name": "Synchronization status", "detail": m.Detail})
	}
	if m.Snapshot.Conflicts > 0 {
		issues = append(issues, map[string]string{"name": fmt.Sprintf("%d conflicts", m.Snapshot.Conflicts), "detail": "Concurrent versions were preserved. Review the files in your sync folder before resolving them."})
	}
	for p, why := range m.Snapshot.Unsupported {
		issues = append(issues, map[string]string{"name": p, "detail": why})
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i]["name"] < issues[j]["name"] })
	return map[string]any{
		"configPath": path, "statePath": cfg.StateDir, "group": cfg.FolderID,
		"peerRows": peers, "transferRows": transfers, "issueRows": issues,
		"heartbeat": snapshotTime(m.Snapshot.Updated), "lastScan": snapshotTime(m.Snapshot.LastScan),
		"uploads": m.Snapshot.ActiveUploads, "downloads": m.Snapshot.ActiveDownloads,
		"canChangeFolder": service.State != status.Running && service.State != status.Starting && service.State != status.Unknown && !m.Fresh,
	}
}

func (a *App) fillWindowData(data map[string]any, path string, cfg configfile.Description, m status.Model, service status.Service, now time.Time) {
	for k, v := range windowData(path, cfg, m, service, now) {
		data[k] = v
	}
	data["busy"] = a.busy.Load()
	data["canStart"] = !a.busy.Load() && path != "" && service.State != status.Unknown && service.State != status.Running && service.State != status.Starting && !m.Fresh
	data["canStop"] = !a.busy.Load() && service.Installed && (service.State == status.Running || service.State == status.Starting || service.State == status.Failed)
	data["canRescan"] = !a.busy.Load() && m.Fresh
	data["installed"] = service.Installed
	data["login"] = service.Enabled
	data["trayLogin"] = a.native.TrayLogin()
}
