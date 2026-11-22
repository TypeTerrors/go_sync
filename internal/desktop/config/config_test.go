package config

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDiscoveryPreservesLegacyAndExplicitSelection(t *testing.T) {
	dir := t.TempDir()
	store := Store{dir}
	if path, err := store.Discover(); err != nil || path != "" {
		t.Fatal(path, err)
	}
	jsonPath := filepath.Join(dir, "default.json")
	yamlPath := filepath.Join(dir, "default.yaml")
	for _, path := range []string{jsonPath, yamlPath} {
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Discover(); err != nil || got != path {
			t.Fatal(got, err)
		}
	}
	custom := filepath.Join(t.TempDir(), "secure.yml")
	if err := os.WriteFile(custom, []byte("tls-key: peer-key.pem"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Select(custom); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Discover(); err != nil || got != custom {
		t.Fatal(got, err)
	}
	for _, p := range []string{jsonPath, yamlPath} {
		data, _ := os.ReadFile(p)
		if string(data) != "original" {
			t.Fatal("existing config overwritten")
		}
	}
	info, _ := os.Stat(filepath.Join(dir, "desktop.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := os.Remove(custom); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Discover(); err == nil || got != custom {
		t.Fatal("must not silently fall back from missing explicit config", got, err)
	}
}
func TestCorruptPreferencesDoNotFallBack(t *testing.T) {
	s := Store{t.TempDir()}
	os.WriteFile(filepath.Join(s.Dir, "desktop.json"), []byte("{"), 0600)
	if _, err := s.Discover(); err == nil {
		t.Fatal("accepted corrupt preferences")
	}
}

type recorder struct {
	args   []string
	output string
}

func (r *recorder) Run(_ context.Context, name string, args ...string) (string, error) {
	r.args = append([]string{name}, args...)
	return r.output, nil
}
func TestClientUsesCLIWithoutShellOrSecurityOverrides(t *testing.T) {
	r := &recorder{output: `{"config_path":"/cfg/secure.yaml","folder":"/files","state_dir":"/history","explicit_state":true,"insecure":false}`}
	c := Client{Binary: "/app/gosync", Run: r}
	cfg, err := c.Resolve(context.Background(), "/cfg/secure.yaml")
	if err != nil || cfg.Insecure || cfg.StateDir != "/history" {
		t.Fatal(cfg, err)
	}
	if len(r.args) != 4 || r.args[1] != "config" {
		t.Fatal(r.args)
	}
	if err = c.Init(context.Background(), "/a folder; $(text)"); err != nil {
		t.Fatal(err)
	}
	if len(r.args) != 4 || r.args[3] != "/a folder; $(text)" {
		t.Fatal(r.args)
	}
	if err = c.InitAt(context.Background(), "/a folder; $(text)", "/private/profile.json"); err != nil {
		t.Fatal(err)
	}
	if len(r.args) != 6 || r.args[3] != "/a folder; $(text)" || r.args[4] != "--config" || r.args[5] != "/private/profile.json" {
		t.Fatal(r.args)
	}
}

func TestImportedProfilesAreRememberedWithoutRewritingConfiguration(t *testing.T) {
	s := Store{t.TempDir()}
	first, second := filepath.Join(t.TempDir(), "secure.yaml"), filepath.Join(t.TempDir(), "other.json")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("original config"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Upgrade a legacy preference that predates the profile registry.
	if err := os.WriteFile(filepath.Join(s.Dir, "desktop.json"), []byte(`{"config":`+strconv.Quote(first)+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{second, first, second} {
		if err := s.Select(path); err != nil {
			t.Fatal(err)
		}
	}
	profiles, err := s.Profiles()
	if err != nil || len(profiles) != 2 || profiles[0] != second || profiles[1] != first {
		t.Fatal(profiles, err)
	}
	for _, path := range profiles {
		data, _ := os.ReadFile(path)
		if string(data) != "original config" {
			t.Fatal("modified existing configuration")
		}
	}
	if got, err := s.Discover(); err != nil || got != second {
		t.Fatal(got, err)
	}
}

func TestSelectionDoesNotResetCorruptPreferences(t *testing.T) {
	s := Store{t.TempDir()}
	path := filepath.Join(s.Dir, "desktop.json")
	os.WriteFile(path, []byte("{"), 0600)
	if err := s.Select(filepath.Join(s.Dir, "new.yaml")); err == nil {
		t.Fatal("overwrote corrupt history registry")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{" {
		t.Fatal(string(data))
	}
}
