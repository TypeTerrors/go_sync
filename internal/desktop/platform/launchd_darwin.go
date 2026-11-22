package platform

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

const daemonLabel = "com.typeterrors.gosync"
const trayLabel = "com.typeterrors.gosync.tray"
const managed = "<!-- Managed by GoSync Desktop -->"

type Native struct {
	Run                Runner
	Home, Binary, Tray string
	UID                int
}

func New(binary, tray string) (*Native, error) {
	home, err := os.UserHomeDir()
	return &Native{Exec{}, home, binary, tray, os.Getuid()}, err
}
func (n *Native) domain() string { return fmt.Sprintf("gui/%d", n.UID) }
func (n *Native) target() string { return n.domain() + "/" + daemonLabel }
func (n *Native) agentPath(label string) string {
	return filepath.Join(n.Home, "Library", "LaunchAgents", label+".plist")
}
func (n *Native) logPath() string {
	return filepath.Join(n.Home, "Library", "Logs", "GoSync", "daemon.log")
}
func (n *Native) ctl(ctx context.Context, args ...string) (string, error) {
	return n.Run.Run(ctx, "launchctl", args...)
}
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func (n *Native) plist(label, binary string, args []string, daemon bool) []byte {
	var a strings.Builder
	for _, v := range append([]string{binary}, args...) {
		a.WriteString("<string>" + xmlText(v) + "</string>")
	}
	options := ""
	if daemon {
		options = "<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict><key>ThrottleInterval</key><integer>5</integer><key>ExitTimeOut</key><integer>20</integer><key>StandardOutPath</key><string>" + xmlText(n.logPath()) + "</string><key>StandardErrorPath</key><string>" + xmlText(n.logPath()) + "</string>"
	}
	return []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + managed + "\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>" + label + "</string><key>ProgramArguments</key><array>" + a.String() + "</array><key>WorkingDirectory</key><string>" + xmlText(n.Home) + "</string><key>RunAtLoad</key><true/><key>ProcessType</key><string>Background</string><key>Umask</key><integer>63</integer>" + options + "</dict></plist>\n")
}
func (n *Native) Inspect(ctx context.Context) (status.Service, error) {
	_, err := os.Stat(n.agentPath(daemonLabel))
	if os.IsNotExist(err) {
		return status.Service{State: status.Stopped}, nil
	}
	if err != nil {
		return status.Service{}, err
	}
	enabledOut, err := n.ctl(ctx, "print-disabled", n.domain())
	if err != nil {
		return status.Service{State: status.Unknown}, err
	}
	s := status.Service{Installed: true, Enabled: !launchdDisabled(enabledOut, daemonLabel), State: status.Stopped}
	if data, err := os.ReadFile(n.agentPath(daemonLabel)); err == nil {
		s.ConfigPath = markedConfig(data)
	}
	out, err := n.ctl(ctx, "print", n.target())
	if err != nil {
		if strings.Contains(out, "Could not find service") || strings.Contains(out, "Could not find specified service") {
			return s, nil
		}
		return s, err
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if !ok {
			continue
		}
		switch key {
		case "state":
			if value == "running" {
				s.State = status.Running
			} else if value == "spawn scheduled" {
				s.State = status.Starting
			}
		case "pid":
			s.PID, _ = strconv.Atoi(value)
		case "last exit code":
			if code, err := strconv.Atoi(value); err == nil && code != 0 {
				s.Detail = "last exit code " + value
			}
		}
	}
	if s.PID == 0 && s.Detail != "" {
		s.State = status.Failed
	}
	return s, nil
}

// launchctl uses true/false on older macOS releases and disabled/enabled on
// newer ones. Match the complete label so the tray job cannot affect the daemon.
func launchdDisabled(output, label string) bool {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=>")
		if ok && strings.TrimSpace(key) == strconv.Quote(label) {
			value = strings.TrimSpace(value)
			return value == "true" || value == "disabled"
		}
	}
	return false
}

func (n *Native) Install(_ context.Context, config string) error {
	if strings.Contains(n.Binary, "/AppTranslocation/") {
		return fmt.Errorf("move GoSync.app to Applications, then reopen it before installing the service")
	}
	if data, err := os.ReadFile(n.agentPath(daemonLabel)); err == nil && !strings.Contains(string(data), managed) {
		return fmt.Errorf("existing custom LaunchAgent is preserved: %s", n.agentPath(daemonLabel))
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(n.logPath()), 0700); err != nil {
		return err
	}
	data := n.plist(daemonLabel, n.Binary, []string{"--config", config}, true)
	data = bytes.Replace(data, []byte(managed), []byte(managed+"\n<!-- "+markConfig(config)+" -->"), 1)
	return WritePrivate(n.agentPath(daemonLabel), data)
}
func (n *Native) Start(ctx context.Context) (result error) {
	s, err := n.Inspect(ctx)
	if err != nil {
		return err
	}
	// launchd's persistent disabled flag also blocks explicit bootstrap. Enable
	// only for the operation, then restore the user's login preference.
	if _, err = n.ctl(ctx, "enable", n.target()); err != nil {
		return err
	}
	if !s.Enabled {
		defer func() {
			restore, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if _, err := n.ctl(restore, "disable", n.target()); err != nil && result == nil {
				result = fmt.Errorf("restore login preference: %w", err)
			}
		}()
	}
	if _, err = n.ctl(ctx, "print", n.target()); err == nil {
		_, err = n.ctl(ctx, "kickstart", n.target())
		return err
	}
	_, err = n.ctl(ctx, "bootstrap", n.domain(), n.agentPath(daemonLabel))
	return err
}
func (n *Native) Stop(ctx context.Context) error {
	if out, err := n.ctl(ctx, "bootout", n.target()); err != nil {
		if strings.Contains(out, "No such process") || strings.Contains(out, "Could not find") {
			return nil
		}
		return err
	}
	return nil
}
func (n *Native) Enable(ctx context.Context, enabled bool) error {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	_, err := n.ctl(ctx, verb, n.target())
	return err
}
func (n *Native) Logs(_ context.Context) (string, error) {
	f, err := os.Open(n.logPath())
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	start := max(int64(0), info.Size()-(256<<10))
	data := make([]byte, info.Size()-start)
	_, err = f.ReadAt(data, start)
	return string(data), err
}
func (n *Native) Open(ctx context.Context, path string) error {
	_, err := n.Run.Run(ctx, "open", path)
	return err
}

func (n *Native) ShowText(ctx context.Context, title, text string) error {
	dir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "GoSync", "Status.txt")
	if err = WritePrivate(path, []byte("GoSync · "+title+"\n\n"+text)); err != nil {
		return err
	}
	_, err = n.Run.Run(ctx, "open", "-a", "TextEdit", path)
	return err
}
func (n *Native) ViewLogs(ctx context.Context) error {
	_, err := n.Run.Run(ctx, "open", "-a", "Console", n.logPath())
	return err
}
func (n *Native) TrayLogin() bool { _, err := os.Stat(n.agentPath(trayLabel)); return err == nil }
func (n *Native) SetTrayLogin(ctx context.Context, enabled bool) error {
	if !enabled {
		return removeOwned(n.agentPath(trayLabel), managed)
	}
	// RunAtLoad takes effect next login. Do not bootstrap a second tray now.
	return writeManaged(n.agentPath(trayLabel), n.plist(trayLabel, n.Tray, nil, false), managed)
}
func (n *Native) Uninstall(ctx context.Context) error {
	data, err := os.ReadFile(n.agentPath(daemonLabel))
	if os.IsNotExist(err) {
		return n.SetTrayLogin(ctx, false)
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), managed) {
		return fmt.Errorf("unmanaged LaunchAgent left untouched")
	}
	if err = n.Stop(ctx); err != nil {
		return err
	}
	if err = removeOwned(n.agentPath(daemonLabel), managed); err != nil {
		return err
	}
	// Clear our disabled override so a later installation has predictable defaults.
	if _, err = n.ctl(ctx, "enable", n.target()); err != nil {
		return err
	}
	return n.SetTrayLogin(ctx, false)
}
