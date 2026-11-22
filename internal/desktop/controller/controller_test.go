package controller

import (
	"context"
	"errors"
	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"github.com/TypeTerrors/go_sync/internal/telemetry"
	"sync"
	"testing"
	"time"
)

type fakeService struct {
	mu                      sync.Mutex
	state                   status.Service
	starts, stops, installs int
	fail                    error
	failState               bool
}

func (f *fakeService) Inspect(context.Context) (status.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *fakeService) Install(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	return nil
}
func (f *fakeService) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.fail != nil {
		return f.fail
	}
	f.state = status.Service{State: status.Running, PID: 42}
	if f.failState {
		f.state.State = status.Failed
		f.state.Detail = "exit 1"
	}
	return nil
}
func (f *fakeService) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.state.State = status.Stopped
	return nil
}
func (*fakeService) Enable(context.Context, bool) error   { return nil }
func (*fakeService) Logs(context.Context) (string, error) { return "", nil }
func (*fakeService) Uninstall(context.Context) error      { return nil }

type fakeDaemon struct {
	cfg     configfile.Description
	rescans int
}

func (d *fakeDaemon) Resolve(context.Context, string) (configfile.Description, error) {
	return d.cfg, nil
}
func (d *fakeDaemon) Rescan(context.Context, string) error { d.rescans++; return nil }
func fixture() (*Controller, *fakeService, *fakeDaemon) {
	f := &fakeService{state: status.Service{State: status.Stopped}}
	d := &fakeDaemon{cfg: configfile.Description{ExplicitState: true, StateDir: "/state", Folder: "/shared"}}
	c := &Controller{Service: f, Daemon: d, PollInterval: time.Millisecond, Busy: func(string) (bool, error) { return false, nil }}
	c.Read = func(string) (telemetry.StatusSnapshot, error) {
		return telemetry.StatusSnapshot{PID: 42, Updated: time.Now(), Running: true, Root: "/shared"}, nil
	}
	return c, f, d
}
func TestStartWaitsForHeartbeatAndConcurrentStartIsIdempotent(t *testing.T) {
	c, f, _ := fixture()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Start(ctx, "/cfg"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.starts != 1 || f.installs != 1 {
		t.Fatal(f.starts, f.installs)
	}
}
func TestExternalOwnershipAndAmbiguousHistoryBlockStart(t *testing.T) {
	for _, test := range []string{"busy", "implicit", "bootstrap"} {
		t.Run(test, func(t *testing.T) {
			c, f, d := fixture()
			switch test {
			case "busy":
				c.Busy = func(string) (bool, error) { return true, nil }
			case "implicit":
				d.cfg.ExplicitState = false
			case "bootstrap":
				d.cfg.Bootstrap = "seed"
			}
			if c.Start(context.Background(), "/cfg") == nil {
				t.Fatal("start allowed")
			}
			if f.starts+f.installs != 0 {
				t.Fatal("service mutated")
			}
		})
	}
}
func TestFailedStartsAndMissingHeartbeat(t *testing.T) {
	for _, kind := range []string{"command", "exited", "heartbeat"} {
		t.Run(kind, func(t *testing.T) {
			c, f, _ := fixture()
			switch kind {
			case "command":
				f.fail = errors.New("start failed")
			case "exited":
				f.failState = true
			case "heartbeat":
				c.Read = func(string) (telemetry.StatusSnapshot, error) {
					return telemetry.StatusSnapshot{}, errors.New("missing")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if c.Start(ctx, "/cfg") == nil {
				t.Fatal("reported false success")
			}
		})
	}
}
func TestStopNeverSignalsAnotherProcess(t *testing.T) {
	c, f, _ := fixture()
	f.state = status.Service{State: status.Running, PID: 999}
	if c.Stop(context.Background(), configfile.Description{StateDir: "/state"}) == nil || f.stops != 0 {
		t.Fatal("stopped unrelated service")
	}
	f.state.PID = 42
	if err := c.Stop(context.Background(), configfile.Description{StateDir: "/state"}); err != nil || f.stops != 1 {
		t.Fatal(err)
	}
}
func TestQuitCancelsUIWorkWithoutStoppingDaemon(t *testing.T) {
	c, f, _ := fixture()
	s := NewSession()
	if err := c.Start(s.Context, "/cfg"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	if s.Context.Err() == nil || f.stops != 0 || f.state.State != status.Running {
		t.Fatal("tray exit affected daemon")
	}
}

func TestStopHungManagedDaemonWithoutHeartbeat(t *testing.T) {
	c, f, _ := fixture()
	f.state = status.Service{State: status.Running, PID: 42, ConfigPath: "/cfg"}
	c.Read = func(string) (telemetry.StatusSnapshot, error) {
		return telemetry.StatusSnapshot{}, errors.New("missing heartbeat")
	}
	if err := c.Stop(context.Background(), configfile.Description{ConfigPath: "/cfg", StateDir: "/state"}); err != nil || f.stops != 1 {
		t.Fatal(err, f.stops)
	}
}
