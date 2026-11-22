//go:build desktop && linux

package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestDashboardHelperProcess(t *testing.T) {
	if os.Getenv("GOSYNC_DASHBOARD_TEST") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var data map[string]any
		if json.Unmarshal(scan.Bytes(), &data) != nil {
			os.Exit(2)
		}
		if data["sample"] != nil {
			fmt.Println(`{"action":"delete-history"}`)
			fmt.Println(`not json`)
			fmt.Println(`{"action":"rescan"}`)
		}
	}
	os.Exit(0)
}

func TestDashboardProcessLifecycleAndProtocol(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	actions := make(chan string, 4)
	p := newDashboardProcess(ctx, actions)
	var starts atomic.Int32
	p.command = func(ctx context.Context) *exec.Cmd {
		starts.Add(1)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDashboardHelperProcess$")
		cmd.Env = append(os.Environ(), "GOSYNC_DASHBOARD_TEST=1")
		return cmd
	}
	errors := make(chan error, 1)
	p.onError = func(err error) { errors <- err }
	p.update([]byte(`{"sample":1}`))
	if err := p.open(); err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if err := p.open(); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatal("duplicate dashboard process")
	}
	select {
	case action := <-actions:
		if action != "rescan" {
			t.Fatalf("unrecognized action accepted: %s", action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not receive initial snapshot")
	}
	// Slow or hidden renderers do not block status polling.
	for i := 0; i < 10000; i++ {
		p.update([]byte(`{"latest":true}`))
	}
	p.close()
	select {
	case err := <-errors:
		t.Fatalf("normal closure reported an error: %v", err)
	default:
	}
	if err := p.open(); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 {
		t.Fatal("dashboard did not reopen")
	}
	cancel()
	p.close()
	if err := p.open(); err == nil {
		t.Fatal("opened dashboard after tray shutdown")
	}
}

func TestDashboardLaunchFailureAndBoundedUpdates(t *testing.T) {
	p := newDashboardProcess(context.Background(), nil)
	p.command = func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, "/missing/gosync-python") }
	if err := p.open(); err == nil {
		t.Fatal("missing renderer was accepted")
	}
	if p.cancel != nil {
		t.Fatal("failed start retained process ownership")
	}
	ch := make(chan []byte, 1)
	replaceUpdate(ch, []byte("old"))
	replaceUpdate(ch, []byte("latest"))
	if string(<-ch) != "latest" {
		t.Fatal("stale UI update retained")
	}
	if dashboardAction("shell") || dashboardAction("reset") {
		t.Fatal("unsafe action accepted")
	}
}

func TestDashboardChildFailureIsReported(t *testing.T) {
	p := newDashboardProcess(context.Background(), nil)
	p.command = func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-c", "raise SystemExit(3)")
	}
	errors := make(chan error, 1)
	p.onError = func(err error) { errors <- err }
	if err := p.open(); err != nil {
		t.Fatal(err)
	}
	defer p.close()
	select {
	case err := <-errors:
		if err == nil {
			t.Fatal("lost child failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("silent renderer failure")
	}
}
