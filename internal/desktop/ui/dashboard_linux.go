//go:build desktop && linux

package ui

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// GTK is provided by Ubuntu's system Python/GObject bindings, like Zenity's
// native dialogs. The embedded renderer has no configuration, service or network
// logic: newline-delimited stdin snapshots in, allowlisted stdout actions out.
//
//go:embed dashboard_linux.py
var dashboardScript string

type dashboardProcess struct {
	ctx     context.Context
	actions chan<- string
	mu      sync.Mutex
	latest  []byte
	cancel  context.CancelFunc
	done    chan struct{}
	updates chan []byte
	show    chan string
	command func(context.Context) *exec.Cmd
	onError func(error)
}

func newDashboardProcess(ctx context.Context, actions chan<- string) *dashboardProcess {
	return &dashboardProcess{ctx: ctx, actions: actions, onError: ReportStartupError, command: func(ctx context.Context) *exec.Cmd {
		// -I ignores PYTHONPATH and user site packages; never execute a shell or
		// an executable supplied by the selected synchronization folder.
		return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-u", "-c", dashboardScript)
	}}
}

func (p *dashboardProcess) update(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latest = data
	if p.updates != nil {
		replaceUpdate(p.updates, data)
	}
}

// A hidden or stalled window must never block daemon polling. Only the newest
// snapshot matters; traffic history is accumulated by Go before this boundary.
func replaceUpdate(ch chan []byte, data []byte) {
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- data:
	default:
	}
}

func (p *dashboardProcess) open() error { return p.openWindow("dashboard") }

func (p *dashboardProcess) openWindow(window string) error {
	if window != "dashboard" && window != "settings" && window != "details" {
		return fmt.Errorf("unknown desktop window")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil {
		return p.ctx.Err()
	}
	if p.cancel != nil {
		select {
		case p.show <- window:
		default:
		}
		return nil
	}
	ctx, cancel := context.WithCancel(p.ctx)
	cmd := p.command(ctx)
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		cancel()
		return err
	}
	var diagnostic cappedDiagnostic
	cmd.Stderr = &diagnostic
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		cancel()
		return fmt.Errorf("open dashboard: install python3-gi, python3-gi-cairo and gir1.2-gtk-3.0: %w", err)
	}
	p.cancel, p.done = cancel, make(chan struct{})
	p.updates, p.show = make(chan []byte, 1), make(chan string, 3)
	updates, show, done := p.updates, p.show, p.done
	show <- window
	if len(p.latest) > 0 {
		updates <- p.latest
	}
	go func() {
		defer input.Close()
		for {
			var data []byte
			select {
			case <-ctx.Done():
				return
			case data = <-updates:
			case window := <-show:
				data, _ = json.Marshal(map[string]string{"showWindow": window})
			}
			if _, err := input.Write(append(append([]byte(nil), data...), '\n')); err != nil {
				return
			}
		}
	}()
	go func() {
		scan := bufio.NewScanner(output)
		scan.Buffer(make([]byte, 1024), 64*1024)
		for scan.Scan() {
			var event struct {
				Action string `json:"action"`
			}
			if json.Unmarshal(scan.Bytes(), &event) == nil && dashboardAction(event.Action) {
				select {
				case p.actions <- event.Action:
				default:
				}
			}
		}
		if scan.Err() != nil {
			cancel()
		}
		err := cmd.Wait()
		unexpected := err != nil && ctx.Err() == nil
		cancel()
		p.mu.Lock()
		p.cancel, p.updates, p.show = nil, nil, nil
		p.mu.Unlock()
		close(done)
		if unexpected && p.ctx.Err() == nil {
			p.onError(fmt.Errorf("dashboard unavailable; install python3-gi, python3-gi-cairo and gir1.2-gtk-3.0: %v\n%s", err, diagnostic.text))
		}
	}()
	return nil
}

func (p *dashboardProcess) close() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	if cancel != nil {
		cancel()
	}
	p.mu.Unlock()
	if cancel != nil {
		<-done
	}
}

func dashboardAction(s string) bool {
	switch s {
	case "open", "rescan", "details", "start", "stop", "settings", "logs", "login", "tray-login", "quit", "config-folder", "remove-service":
		return true
	}
	for _, prefix := range []string{"choose-folder:", "choose-config:"} {
		if path, ok := strings.CutPrefix(s, prefix); ok {
			return filepath.IsAbs(path) && !strings.ContainsRune(path, 0) && len(path) <= 4096
		}
	}
	return false
}

type cappedDiagnostic struct{ text string }

func (b *cappedDiagnostic) Write(data []byte) (int, error) {
	if len(b.text) < 4096 {
		b.text += string(data[:min(len(data), 4096-len(b.text))])
	}
	return len(data), nil
}
