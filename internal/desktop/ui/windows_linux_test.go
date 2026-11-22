//go:build desktop && linux

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/config"
	"github.com/TypeTerrors/go_sync/internal/desktop/platform"
	"golang.org/x/sys/unix"
)

type profileRunner struct {
	configs     map[string]configfile.Description
	active      string
	initializes int
}

func (r *profileRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "systemctl" {
		return "LoadState=loaded\nActiveState=" + r.active + "\nMainPID=0\n", nil
	}
	if len(args) > 0 && args[0] == "init" {
		r.initializes++
		return "", fmt.Errorf("unexpected new history")
	}
	if len(args) == 3 && args[0] == "config" {
		cfg, ok := r.configs[args[2]]
		if !ok {
			return "", fmt.Errorf("configuration unavailable")
		}
		b, err := json.Marshal(cfg)
		return string(b), err
	}
	return "", fmt.Errorf("unexpected operation: %s %v", name, args)
}
func profileApp(t *testing.T) (*App, *profileRunner, string, string) {
	t.Helper()
	root := t.TempDir()
	runner := &profileRunner{configs: map[string]configfile.Description{}, active: "inactive"}
	a := &App{store: config.Store{Dir: filepath.Join(root, "preferences")}, client: config.Client{Binary: "daemon", Run: runner}, native: &platform.Native{Run: runner, ConfigHome: root}}
	paths := []string{}
	for _, name := range []string{"secure", "other"} {
		folder, state, path := filepath.Join(root, name), filepath.Join(root, name+"-state"), filepath.Join(root, name+".yaml")
		os.Mkdir(folder, 0700)
		os.Mkdir(state, 0700)
		os.WriteFile(path, []byte("preserve identity"), 0600)
		runner.configs[path] = configfile.Description{ConfigPath: path, Folder: folder, StateDir: state, ExplicitState: true, Insecure: false}
		if err := a.load(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return a, runner, paths[0], paths[1]
}
func TestFolderPickerReusesImportedHistory(t *testing.T) {
	a, r, first, _ := profileApp(t)
	cfg := r.configs[first]
	if err := a.createFolderProfile(context.Background(), cfg.Folder); err != nil {
		t.Fatal(err)
	}
	path, got := a.selection()
	if path != first || got.StateDir != cfg.StateDir || got.Insecure || r.initializes != 0 {
		t.Fatal(path, got, r.initializes)
	}
	if entries, _ := os.ReadDir(filepath.Join(a.store.Dir, "folders")); len(entries) != 0 {
		t.Fatal("created replacement history")
	}
}
func TestFolderSwitchCannotBypassActiveServiceOrState(t *testing.T) {
	for _, target := range []string{"service", "selected-state", "target-state"} {
		t.Run(target, func(t *testing.T) {
			a, r, first, second := profileApp(t)
			if target == "service" {
				r.active = "active"
			} else {
				cfg := r.configs[first]
				if target == "selected-state" {
					cfg = r.configs[second]
				}
				f, err := os.Open(cfg.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
			}
			if err := a.createFolderProfile(context.Background(), r.configs[first].Folder); err == nil {
				t.Fatal("switched active folder")
			}
			if path, _ := a.selection(); path != second || r.initializes != 0 {
				t.Fatal("selection or history changed")
			}
		})
	}
}
func TestUnverifiableProfileCannotSilentlyCreateReplacementHistory(t *testing.T) {
	a, r, first, second := profileApp(t)
	folder := r.configs[first].Folder
	delete(r.configs, first)
	if err := a.createFolderProfile(context.Background(), folder); err == nil {
		t.Fatal("ignored broken profile")
	}
	if path, _ := a.selection(); path != second || r.initializes != 0 {
		t.Fatal(path, r.initializes)
	}
}
func TestRendererAcceptsOnlyLocalPickerActions(t *testing.T) {
	for _, action := range []string{"choose-folder:/tmp/a folder; literal", "choose-config:/tmp/name\n.yaml", "config-folder", "remove-service"} {
		if !dashboardAction(action) {
			t.Fatal("rejected native action", action)
		}
	}
	for _, action := range []string{"choose-folder:", "choose-config:relative.yaml", "choose-folder:file:///tmp/a", "choose-folder:/tmp/\x00", "delete-history"} {
		if dashboardAction(action) {
			t.Fatal("accepted invalid action", action)
		}
	}
}
