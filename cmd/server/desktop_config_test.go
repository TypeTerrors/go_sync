package main

import (
	"encoding/json"
	"github.com/TypeTerrors/go_sync/internal/configfile"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopDescriptionUsesDaemonParsingWithoutOpeningState(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(config, []byte(`{"sync-folder":"shared","state-dir":"history","insecure":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	out, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = out
	defer func() { os.Stdout = original; out.Close() }()
	if err = runDaemon([]string{"--config", config}, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.Name())
	var description configfile.Description
	if err = json.Unmarshal(data, &description); err != nil {
		t.Fatal(err, string(data))
	}
	if description.StateDir != filepath.Join(dir, "history") || !description.ExplicitState || !description.Insecure {
		t.Fatal(description)
	}
	if _, err = os.Stat(description.StateDir); !os.IsNotExist(err) {
		t.Fatal("configuration read opened or created history")
	}
}
