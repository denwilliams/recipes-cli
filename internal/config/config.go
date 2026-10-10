// Package config persists explicit provider overrides. Missing files are read-only defaults.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/denwilliams/recipes-cli/internal/fault"
)

type Config struct {
	Version   int             `json:"version"`
	Providers map[string]bool `json:"providers"`
}

func Default() Config { return Config{1, map[string]bool{}} }
func Path(explicit *string, env func(string) (string, bool)) (string, error) {
	override, exists := env("RECIPES_CONFIG")
	if explicit != nil {
		override, exists = *explicit, true
	}
	if exists {
		if strings.TrimSpace(override) == "" {
			return "", fault.New("CONFIG", "Config path is empty. Set --config or RECIPES_CONFIG to a file path.")
		}
		path, err := filepath.Abs(override)
		if err != nil {
			return "", fault.New("CONFIG", "Cannot resolve config path.")
		}
		return path, nil
	}
	dir, exists := env("XDG_CONFIG_HOME")
	if exists && !filepath.IsAbs(dir) {
		return "", fault.New("CONFIG", "XDG_CONFIG_HOME must be an absolute directory.")
	}
	if !exists {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fault.New("CONFIG", "Cannot determine home directory; set --config.")
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "recipes-cli", "config.json"), nil
}

type Store struct {
	Path string
	IDs  []string
}

func (s Store) known(id string) bool {
	for _, candidate := range s.IDs {
		if candidate == id {
			return true
		}
	}
	return false
}
func (s Store) Load() (Config, error) {
	raw, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fault.New("CONFIG", "Cannot read config at %s. Check path and file access.", s.Path)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Config{}, fault.New("CONFIG", "Invalid JSON at %s. Fix it or move it aside to restore defaults.", s.Path)
	}
	invalid := func() (Config, error) {
		return Config{}, fault.New("CONFIG", "Invalid config at %s. Expected {\"version\":1,\"providers\":{\"provider-id\":true|false}}.", s.Path)
	}
	if len(fields) != 2 {
		return invalid()
	}
	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil || version != 1 {
		return invalid()
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(fields["providers"], &providers); err != nil || providers == nil {
		return invalid()
	}
	cfg := Default()
	for id, value := range providers {
		if !s.known(id) {
			return Config{}, fault.New("CONFIG", "Unknown provider %q in %s. Available: %s.", id, s.Path, strings.Join(s.IDs, ", "))
		}
		v := strings.TrimSpace(string(value))
		if v != "true" && v != "false" {
			return invalid()
		}
		cfg.Providers[id] = v == "true"
	}
	return cfg, nil
}
func (s Store) SetEnabled(id string, enabled bool) (err error) {
	if !s.known(id) {
		return fault.New("PROVIDER", "Unknown provider %q. Available: %s.", id, strings.Join(s.IDs, ", "))
	}
	cfg, err := s.Load()
	if err != nil {
		return err
	}
	cfg.Providers[id] = enabled
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	defer func() {
		if err != nil {
			err = fault.New("CONFIG", "Cannot save config at %s. Check directory/file access.", s.Path)
		}
	}()
	if err = os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".recipes-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), s.Path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
