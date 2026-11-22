package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/platform"
)

type Client struct {
	Binary string
	Run    platform.Runner
}

func (c Client) Resolve(ctx context.Context, path string) (configfile.Description, error) {
	var cfg configfile.Description
	data, err := c.Run.Run(ctx, c.Binary, "config", "--config", path)
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal([]byte(data), &cfg); err != nil {
		return cfg, fmt.Errorf("read daemon configuration: %w", err)
	}
	if !filepath.IsAbs(cfg.ConfigPath) || !filepath.IsAbs(cfg.StateDir) || !filepath.IsAbs(cfg.Folder) {
		return cfg, fmt.Errorf("daemon returned unresolved configuration paths")
	}
	return cfg, nil
}
func (c Client) Init(ctx context.Context, folder string) error {
	_, err := c.Run.Run(ctx, c.Binary, "init", "--sync-folder", folder)
	return err
}

// InitAt creates a separate profile; init refuses to overwrite an existing file.
func (c Client) InitAt(ctx context.Context, folder, path string) error {
	_, err := c.Run.Run(ctx, c.Binary, "init", "--sync-folder", folder, "--config", path)
	return err
}
func (c Client) Rescan(ctx context.Context, state string) error {
	_, err := c.Run.Run(ctx, c.Binary, "rescan", "--state-dir", state, "--wait", "30s")
	return err
}

type Preferences struct {
	Config       string   `json:"config"`
	KnownConfigs []string `json:"knownConfigs,omitempty"`
}
type Store struct{ Dir string }

func DefaultStore() (Store, error) {
	p, err := configfile.DefaultPath()
	return Store{filepath.Dir(p)}, err
}
func (s Store) Discover() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "desktop.json"))
	if err == nil {
		var p Preferences
		if err = json.Unmarshal(data, &p); err != nil {
			return "", fmt.Errorf("desktop preferences: %w", err)
		}
		if !filepath.IsAbs(p.Config) {
			return "", fmt.Errorf("desktop preferences need an absolute configuration path")
		}
		if _, err = os.Stat(p.Config); err != nil {
			return p.Config, err
		}
		return p.Config, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	for _, name := range []string{"default.yaml", "default.json"} {
		path := filepath.Join(s.Dir, name)
		if _, err = os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", nil
}
func (s Store) Select(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(path), ".yaml") && !strings.EqualFold(filepath.Ext(path), ".yml") && !strings.EqualFold(filepath.Ext(path), ".json") {
		return fmt.Errorf("choose a YAML or JSON configuration")
	}
	known, err := s.Profiles()
	if err != nil {
		return err
	}
	known = append([]string{path}, known...)
	seen := map[string]bool{}
	unique := []string{}
	for _, candidate := range known {
		if !seen[candidate] {
			unique = append(unique, candidate)
			seen[candidate] = true
		}
	}
	data, err := json.MarshalIndent(Preferences{Config: path, KnownConfigs: unique}, "", "  ")
	if err != nil {
		return err
	}
	return platform.WritePrivate(filepath.Join(s.Dir, "desktop.json"), append(data, '\n'))
}

// Profiles includes imported selections, desktop-created profiles and legacy
// defaults. Only paths are remembered; no configuration or identity is copied.
func (s Store) Profiles() ([]string, error) {
	var known []string
	data, err := os.ReadFile(filepath.Join(s.Dir, "desktop.json"))
	if err == nil {
		var p Preferences
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("desktop preferences: %w", err)
		}
		known = append(known, p.Config)
		known = append(known, p.KnownConfigs...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, name := range []string{"default.yaml", "default.json"} {
		path := filepath.Join(s.Dir, name)
		if _, err := os.Stat(path); err == nil {
			known = append(known, path)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	profiles, err := filepath.Glob(filepath.Join(s.Dir, "folders", "workspace-*", "config.json"))
	if err != nil {
		return nil, err
	}
	known = append(known, profiles...)
	seen := map[string]bool{}
	result := []string{}
	for _, path := range known {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("desktop profile paths must be absolute")
		}
		if !seen[path] {
			result = append(result, path)
			seen[path] = true
		}
	}
	return result, nil
}
