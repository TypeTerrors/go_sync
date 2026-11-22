//go:build desktop && (linux || darwin)

package main

import (
	"fmt"
	"github.com/TypeTerrors/go_sync/internal/desktop/ui"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		ui.ReportStartupError(err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 {
		return fmt.Errorf("GoSync Desktop takes no arguments; choose configuration in Settings")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	binary := filepath.Join(filepath.Dir(executable), "gosync")
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("companion gosync daemon is missing beside the desktop executable; reinstall the desktop package")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err = os.Chdir(home); err != nil {
		return err
	}
	return ui.Run(binary, executable)
}
