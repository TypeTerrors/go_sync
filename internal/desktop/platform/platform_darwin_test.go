package platform

import (
	"context"
	"errors"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls  [][]string
	output string
	err    error
}

type launchRunner struct {
	calls            [][]string
	loaded, disabled bool
	wordOverrides    bool
	failure          error
}

func (r *launchRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	switch args[0] {
	case "print-disabled":
		if r.disabled {
			if r.wordOverrides {
				return `"com.typeterrors.gosync" => disabled`, nil
			}
			return `"com.typeterrors.gosync" => true`, nil
		}
		return "", nil
	case "print":
		if r.loaded {
			return "state = running\npid = 123\n", nil
		}
		return "Could not find service", errors.New("not loaded")
	case "enable":
		r.disabled = false
	case "disable":
		r.disabled = true
	case "bootstrap":
		if r.failure != nil {
			return "", r.failure
		}
		r.loaded = true
	case "bootout":
		r.loaded = false
	}
	return "", nil
}
func TestLaunchdExplicitStartPreservesLoginPreference(t *testing.T) {
	for _, words := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			r := &launchRunner{disabled: true, wordOverrides: words}
			if fail {
				r.failure = errors.New("bootstrap denied")
			}
			n := Native{Run: r, Home: t.TempDir(), Binary: "/app/gosync", UID: 501}
			if err := n.Install(context.Background(), "/cfg"); err != nil {
				t.Fatal(err)
			}
			err := n.Start(context.Background())
			if (err != nil) != fail {
				t.Fatal(err)
			}
			if !r.disabled {
				t.Fatal("explicit start changed login preference")
			}
			for _, call := range r.calls {
				if call[0] != "launchctl" || len(call) < 3 || !strings.HasPrefix(call[2], "gui/501") {
					t.Fatal("control escaped user GUI domain", call)
				}
			}
			if !fail {
				s, err := n.Inspect(context.Background())
				if err != nil || s.State != status.Running || s.PID != 123 || s.ConfigPath != "/cfg" || s.Enabled {
					t.Fatal(s, err)
				}
				if err = n.Stop(context.Background()); err != nil || r.loaded {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestLaunchdDisabledFormats(t *testing.T) {
	for _, tc := range []struct {
		output   string
		disabled bool
	}{
		{`"com.typeterrors.gosync" => true`, true},
		{`"com.typeterrors.gosync" => disabled`, true},
		{"\tdisabled services = {\n\t\t\"com.typeterrors.gosync\" => disabled\n\t}\n", true},
		{`"com.typeterrors.gosync" => false`, false},
		{`"com.typeterrors.gosync" => enabled`, false},
		{`"com.typeterrors.gosync.tray" => disabled`, false},
		{"", false},
	} {
		if got := launchdDisabled(tc.output, daemonLabel); got != tc.disabled {
			t.Errorf("launchdDisabled(%q) = %v; want %v", tc.output, got, tc.disabled)
		}
	}
}

func TestLaunchdNeverExitedIsNotAFailure(t *testing.T) {
	for _, tc := range []struct {
		code  string
		state status.ServiceState
	}{
		{"(never exited)", status.Stopped},
		{"0", status.Stopped},
		{"1", status.Failed},
	} {
		r := &recordingRunner{output: "state = not running\nlast exit code = " + tc.code + "\n"}
		n := Native{Run: r, Home: t.TempDir(), Binary: "/app/gosync", UID: 501}
		if err := n.Install(context.Background(), "/cfg"); err != nil {
			t.Fatal(err)
		}
		s, err := n.Inspect(context.Background())
		if err != nil || s.State != tc.state || (tc.code != "1" && s.Detail != "") {
			t.Fatalf("exit code %q: %+v, %v", tc.code, s, err)
		}
	}
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.output, r.err
}
func TestLaunchAgentEscapesPathsAndPassesPlutil(t *testing.T) {
	n := Native{Run: &recordingRunner{}, Home: t.TempDir(), Binary: "/Applications/A & B/GoSync.app/Contents/MacOS/gosync", Tray: "/Applications/A & B/GoSync.app/Contents/MacOS/gosync-desktop", UID: 501}
	if err := n.Install(context.Background(), "/a <folder>/default.yaml"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(n.agentPath(daemonLabel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "A &amp; B") || !strings.Contains(string(data), "a &lt;folder&gt;") {
		t.Fatal(string(data))
	}
	if out, err := exec.Command("plutil", "-lint", n.agentPath(daemonLabel)).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if err = n.SetTrayLogin(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-lint", n.agentPath(trayLabel)).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if !n.TrayLogin() {
		t.Fatal("login integration missing")
	}
	if err = n.SetTrayLogin(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}
func TestLaunchAgentProtectsExistingDefinitions(t *testing.T) {
	n := Native{Home: t.TempDir()}
	p := n.agentPath(daemonLabel)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("custom"), 0600)
	if n.Install(context.Background(), "/cfg") == nil {
		t.Fatal("custom agent overwritten")
	}
	if n.Uninstall(context.Background()) == nil {
		t.Fatal("custom agent removed")
	}
}
