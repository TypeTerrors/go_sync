package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TypeTerrors/go_sync/deploy"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

const managed = "# Managed by GoSync Desktop\n"
const unit = "gosync.service"

type Native struct {
	Run                            Runner
	Home, ConfigHome, Binary, Tray string
}

func New(binary, tray string) (*Native, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Native{Exec{}, home, config, binary, tray}, nil
}
func (n *Native) unitPath() string { return filepath.Join(n.ConfigHome, "systemd", "user", unit) }
func (n *Native) ctl(ctx context.Context, args ...string) (string, error) {
	return n.Run.Run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}
func (n *Native) Inspect(ctx context.Context) (status.Service, error) {
	out, err := n.ctl(ctx, "show", unit, "--property=LoadState,ActiveState,SubState,MainPID,UnitFileState,Result")
	if err != nil {
		return status.Service{State: status.Unknown}, err
	}
	s := ParseSystemd(out)
	if data, err := os.ReadFile(n.unitPath()); err == nil {
		s.ConfigPath = markedConfig(data)
	}
	return s, nil
}
func ParseSystemd(out string) status.Service {
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			values[k] = v
		}
	}
	s := status.Service{Installed: values["LoadState"] == "loaded", Enabled: values["UnitFileState"] == "enabled", Detail: values["SubState"] + "; result=" + values["Result"], State: status.Unknown}
	s.PID, _ = strconv.Atoi(values["MainPID"])
	switch values["ActiveState"] {
	case "active":
		s.State = status.Running
	case "activating", "deactivating":
		s.State = status.Starting
	case "failed":
		s.State = status.Failed
	case "inactive":
		s.State = status.Stopped
	}
	if values["LoadState"] == "bad-setting" || values["LoadState"] == "error" {
		s.State = status.Failed
		s.Detail = "invalid service definition: " + values["LoadState"]
	}
	if values["LoadState"] == "not-found" {
		s.State = status.Stopped
	}
	return s
}
func unitQuote(value string) (string, error) {
	if strings.ContainsAny(value, "\x00\n\r") {
		return "", fmt.Errorf("service paths cannot contain newlines or NUL")
	}
	value = strings.ReplaceAll(value, "%", "%%")
	return strconv.Quote(value), nil
}
func (n *Native) Install(ctx context.Context, config string) error {
	binary, err := unitQuote(n.Binary)
	if err != nil {
		return err
	}
	path, err := unitQuote(config)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(n.unitPath())
	if err == nil && !strings.HasPrefix(string(data), managed) {
		if string(data) != deploy.DefaultService {
			return fmt.Errorf("existing custom %s is preserved; migrate it explicitly before desktop service installation", n.unitPath())
		}
		backup := n.unitPath() + ".before-desktop"
		if _, err = os.Stat(backup); os.IsNotExist(err) {
			if err = WritePrivate(backup, data); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("service backup already exists or cannot be inspected: %s", backup)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := managed + "# " + markConfig(config) + "\n[Unit]\nDescription=GoSync shared folder\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=:" + binary + " --config " + path + "\nWorkingDirectory=%h\nRestart=on-failure\nRestartSec=3\nTimeoutStopSec=20\nKillSignal=SIGTERM\nNoNewPrivileges=true\nUMask=0077\n\n[Install]\nWantedBy=default.target\n"
	if err = WritePrivate(n.unitPath(), []byte(text)); err != nil {
		return err
	}
	_, err = n.ctl(ctx, "daemon-reload")
	return err
}
func (n *Native) Start(ctx context.Context) error { _, err := n.ctl(ctx, "start", unit); return err }
func (n *Native) Stop(ctx context.Context) error  { _, err := n.ctl(ctx, "stop", unit); return err }
func (n *Native) Enable(ctx context.Context, enabled bool) error {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	_, err := n.ctl(ctx, verb, unit)
	return err
}
func (n *Native) Logs(ctx context.Context) (string, error) {
	return n.Run.Run(ctx, "journalctl", "--user", "--unit", unit, "--lines=250", "--no-pager", "--output=short-iso")
}

func (n *Native) ShowText(ctx context.Context, title, text string) error {
	f, err := os.CreateTemp("", "gosync-desktop-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	_, err = n.Run.Run(ctx, "zenity", "--text-info", "--title=GoSync · "+title, "--filename="+f.Name(), "--width=900", "--height=600")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil
	}
	return err
}
func (n *Native) ViewLogs(ctx context.Context) error {
	text, err := n.Logs(ctx)
	if err != nil {
		return err
	}
	return n.ShowText(ctx, "Recent daemon logs", text)
}
func (n *Native) Open(ctx context.Context, path string) error {
	_, err := n.Run.Run(ctx, "xdg-open", path)
	return err
}
func (n *Native) SetTrayLogin(_ context.Context, enabled bool) error {
	path := filepath.Join(n.ConfigHome, "autostart", "gosync-desktop.desktop")
	if !enabled {
		return removeOwned(path, managed)
	}
	executable, err := desktopQuote(n.Tray)
	if err != nil {
		return err
	}
	return writeManaged(path, []byte(managed+"[Desktop Entry]\nType=Application\nName=GoSync Desktop\nExec="+executable+"\nIcon=gosync\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"), managed)
}
func (n *Native) TrayLogin() bool {
	_, err := os.Stat(filepath.Join(n.ConfigHome, "autostart", "gosync-desktop.desktop"))
	return err == nil
}
func desktopQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\x00\n\r") {
		return "", fmt.Errorf("invalid desktop executable path")
	}
	s = strings.NewReplacer("\\", "\\\\\\\\", "\"", "\\\\\"", "`", "\\\\`", "$", "\\\\$", "%", "%%").Replace(s)
	return "\"" + s + "\"", nil
}
func (n *Native) Uninstall(ctx context.Context) error {
	data, err := os.ReadFile(n.unitPath())
	if os.IsNotExist(err) {
		return n.SetTrayLogin(ctx, false)
	}
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(data), managed) {
		return fmt.Errorf("this service was not installed by GoSync Desktop; left untouched")
	}
	if err = n.Stop(ctx); err != nil {
		return err
	}
	if err = n.Enable(ctx, false); err != nil {
		return err
	}
	if err = removeOwned(n.unitPath(), managed); err != nil {
		return err
	}
	backup := n.unitPath() + ".before-desktop"
	if _, err = os.Stat(backup); err == nil {
		if err = os.Rename(backup, n.unitPath()); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err = n.ctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	return n.SetTrayLogin(ctx, false)
}
