package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TypeTerrors/go_sync/deploy"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

type recordingRunner struct {
	calls  [][]string
	output string
	err    error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.output, r.err
}
func TestSystemdClassification(t *testing.T) {
	for _, tc := range []struct {
		active string
		want   status.ServiceState
	}{{"active", status.Running}, {"activating", status.Starting}, {"deactivating", status.Starting}, {"failed", status.Failed}, {"inactive", status.Stopped}, {"unknown", status.Unknown}} {
		s := ParseSystemd("LoadState=loaded\nActiveState=" + tc.active + "\nMainPID=123\nUnitFileState=enabled\n")
		if s.State != tc.want || s.PID != 123 || !s.Enabled || !s.Installed {
			t.Fatal(s)
		}
	}
	if ParseSystemd("LoadState=not-found").State != status.Stopped {
		t.Fatal("missing unit")
	}
}
func TestSystemdAdoptsOnlyOriginalCLIUnitAndRestoresIt(t *testing.T) {
	dir := t.TempDir()
	run := &recordingRunner{}
	n := Native{Run: run, Home: dir, ConfigHome: filepath.Join(dir, "config"), Binary: "/an app/$literal%/gosync", Tray: "/an app/gosync-desktop"}
	if err := WritePrivate(n.unitPath(), []byte(deploy.DefaultService)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := n.Install(ctx, "/cfg/a b.yaml"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(n.unitPath())
	if !strings.Contains(string(data), `ExecStart=:"/an app/$literal%%/gosync" --config "/cfg/a b.yaml"`) {
		t.Fatal(string(data))
	}
	if err := n.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(n.unitPath())
	if string(data) != deploy.DefaultService {
		t.Fatal("original service was not restored")
	}
	if err := WritePrivate(n.unitPath(), []byte("custom service")); err != nil {
		t.Fatal(err)
	}
	if n.Install(ctx, "/cfg") == nil {
		t.Fatal("overwrote custom unit")
	}
	if n.Uninstall(ctx) == nil {
		t.Fatal("removed custom unit")
	}
}
func TestServiceCommandsStayInUserManager(t *testing.T) {
	r := &recordingRunner{}
	n := Native{Run: r}
	for _, f := range []func(context.Context) error{n.Start, n.Stop, func(ctx context.Context) error { return n.Enable(ctx, true) }} {
		if err := f(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, call := range r.calls {
		if len(call) < 4 || call[0] != "systemctl" || call[1] != "--user" || call[len(call)-1] != "gosync.service" {
			t.Fatal(call)
		}
	}
}
func TestPathsRejectUnitInjection(t *testing.T) {
	for _, path := range []string{"/a\nExecStart=/evil", "/a\r", "/a\x00b"} {
		if _, err := unitQuote(path); err == nil {
			t.Fatal(path)
		}
		if _, err := desktopQuote(path); err == nil {
			t.Fatal(path)
		}
	}
}

func TestGeneratedUnitPassesSystemdValidation(t *testing.T) {
	tool, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze unavailable")
	}
	root := t.TempDir()
	n := Native{Run: &recordingRunner{}, Home: root, ConfigHome: root, Binary: "/usr/bin/true"}
	if err = n.Install(context.Background(), "/a config/100%.yaml"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(tool, "verify", n.unitPath()).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	data, _ := os.ReadFile(n.unitPath())
	if markedConfig(data) != "/a config/100%.yaml" {
		t.Fatal("configuration identity lost")
	}
}
