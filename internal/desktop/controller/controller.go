// Package controller coordinates a per-user OS service. It never owns the daemon
// process: closing the controller only cancels UI work, never synchronization.
package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"github.com/TypeTerrors/go_sync/internal/telemetry"
)

type Service interface {
	Inspect(context.Context) (status.Service, error)
	Install(context.Context, string) error
	Start(context.Context) error
	Stop(context.Context) error
	Enable(context.Context, bool) error
	Logs(context.Context) (string, error)
	Uninstall(context.Context) error
}
type Daemon interface {
	Resolve(context.Context, string) (configfile.Description, error)
	Rescan(context.Context, string) error
}

type Controller struct {
	Service      Service
	Daemon       Daemon
	Read         func(string) (telemetry.StatusSnapshot, error)
	Busy         func(string) (bool, error)
	PollInterval time.Duration
	mu           sync.Mutex
}

// Start requires an explicit persistent state path so a GUI launch cannot adopt
// a different database merely because its working directory differs from a CLI.
func (c *Controller) Start(ctx context.Context, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg, err := c.Daemon.Resolve(ctx, path)
	if err != nil {
		return err
	}
	if !cfg.ExplicitState {
		return fmt.Errorf("set state-dir to the existing database path in your configuration before using desktop service controls")
	}
	if cfg.Bootstrap != "" {
		return fmt.Errorf("remove the one-time bootstrap setting after initializing history; desktop startup never resets history")
	}
	state, err := c.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	snapshot, readErr := c.Read(cfg.StateDir)
	if state.State == status.Running || state.State == status.Starting {
		if readErr == nil && status.Fresh(snapshot, time.Now()) && status.Matches(snapshot, cfg.Folder, cfg.FolderID) && state.PID > 0 && snapshot.PID == state.PID {
			return nil
		}
		return fmt.Errorf("the service is already active; check its status and configuration before starting another instance")
	}
	if state.State == status.Failed {
		if err = c.Service.Stop(ctx); err != nil {
			return err
		}
	}
	busy, err := c.Busy(cfg.StateDir)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("another process owns this state directory; stop that CLI or service first")
	}
	if err = c.Service.Install(ctx, path); err != nil {
		return err
	}
	started := time.Now()
	if err = c.Service.Start(ctx); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	interval := c.PollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		state, err = c.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if state.State == status.Failed {
			return fmt.Errorf("service start failed: %s; see View Logs", state.Detail)
		}
		s, err := c.Read(cfg.StateDir)
		if err == nil && status.Fresh(s, time.Now()) && status.Matches(s, cfg.Folder, cfg.FolderID) && s.Updated.After(started) && s.PID == state.PID && state.State == status.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("no fresh daemon heartbeat before the startup deadline; inspect View Logs: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *Controller) Stop(ctx context.Context, cfg configfile.Description) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if state.State == status.Stopped {
		return nil
	}
	s, readErr := c.Read(cfg.StateDir)
	// The OS service identity scopes the stop, never a PID from an untrusted file.
	// A matching installed configuration also permits stopping a hung daemon
	// that never produced a heartbeat. It is never necessary to kill a raw PID.
	knownConfig := cfg.ConfigPath != "" && state.ConfigPath == cfg.ConfigPath
	if state.PID > 0 && !knownConfig && (readErr != nil || s.PID != state.PID) {
		return fmt.Errorf("the selected state belongs to another process; it will not be stopped by the tray")
	}
	return c.Service.Stop(ctx)
}

func (c *Controller) Rescan(ctx context.Context, path string) error {
	cfg, err := c.Daemon.Resolve(ctx, path)
	if err != nil {
		return err
	}
	return c.Daemon.Rescan(ctx, cfg.StateDir)
}

// Session provides a cancellable UI lifetime, intentionally with no Stop call.
type Session struct {
	Context context.Context
	cancel  context.CancelFunc
}

func NewSession() *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{ctx, cancel}
}
func (s *Session) Close() { s.cancel() }
