//go:build linux || darwin

package platform

import (
	"github.com/TypeTerrors/go_sync/internal/servers"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStateLockAcrossProcesses(t *testing.T) {
	if state := os.Getenv("GOSYNC_TEST_BUSY_STATE"); state != "" {
		busy, err := StateBusy(state)
		if err != nil || !busy {
			t.Fatalf("busy=%v err=%v", busy, err)
		}
		return
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	folder := filepath.Join(root, "files")
	engine, err := servers.NewEngine(folder, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	if other, err := servers.NewEngine(folder, state); err == nil {
		other.Close()
		t.Fatal("second engine acquired history")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStateLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "GOSYNC_TEST_BUSY_STATE="+state)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
}
func TestTraySingletonDoesNotLockDaemonState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.lock")
	f, err := LockTray(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockTray(path); err == nil {
		second.Close()
		t.Fatal("second tray acquired lock")
	}
	f.Close()
	second, err := LockTray(path)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	busy, err := StateBusy(filepath.Dir(path))
	if err != nil || busy {
		t.Fatal("tray locked daemon directory", err)
	}
}
func TestPrivateFilesDoNotReplaceSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original")
	os.WriteFile(target, []byte("keep"), 0600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if WritePrivate(link, []byte("replace")) == nil {
		t.Fatal("replaced symlink")
	}
	if f, err := LockTray(link); err == nil {
		f.Close()
		t.Fatal("followed symlink")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "keep" {
		t.Fatal("target modified")
	}
}
