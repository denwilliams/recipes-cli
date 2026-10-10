package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/denwilliams/recipes-cli/internal/fault"
)

func env(values map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := values[k]; return v, ok }
}
func TestConfigPathPrecedence(t *testing.T) {
	explicit := "explicit.json"
	abs, _ := filepath.Abs(explicit)
	p, err := Path(&explicit, env(map[string]string{"RECIPES_CONFIG": "/env.json", "XDG_CONFIG_HOME": "relative"}))
	if err != nil || p != abs {
		t.Fatal(p, err)
	}
	p, err = Path(nil, env(map[string]string{"RECIPES_CONFIG": "/env.json"}))
	if err != nil || p != "/env.json" {
		t.Fatal(p, err)
	}
	p, err = Path(nil, env(map[string]string{"XDG_CONFIG_HOME": "/xdg"}))
	if err != nil || p != "/xdg/recipes-cli/config.json" {
		t.Fatal(p, err)
	}
	p, err = Path(nil, env(nil))
	home, _ := os.UserHomeDir()
	if err != nil || p != filepath.Join(home, ".config", "recipes-cli", "config.json") {
		t.Fatal(p, err)
	}
	for _, values := range []map[string]string{{"RECIPES_CONFIG": ""}, {"XDG_CONFIG_HOME": ""}, {"XDG_CONFIG_HOME": "relative"}} {
		if _, err := Path(nil, env(values)); err == nil {
			t.Fatal("bad config path accepted")
		}
	}
}
func TestReadOnlyDefaultsAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	s := Store{path, []string{"a", "b"}}
	cfg, err := s.Load()
	if err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatal(cfg, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("load wrote config")
	}
	if err := s.SetEnabled("a", false); err != nil {
		t.Fatal(err)
	}
	fresh := Store{path, []string{"a", "b"}}
	if err := fresh.SetEnabled("b", true); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Load()
	if err != nil || !reflect.DeepEqual(cfg.Providers, map[string]bool{"a": false, "b": true}) {
		t.Fatal(cfg, err)
	}
	if err := s.SetEnabled("a", true); err != nil {
		t.Fatal(err)
	}
	cfg, _ = fresh.Load()
	if !cfg.Providers["a"] {
		t.Fatal("not persisted")
	}
	entries, _ = os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("temp residue", entries)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config not private", info.Mode())
	}
	var e *fault.Error
	if err := s.SetEnabled("unknown", false); !errors.As(err, &e) || e.Code != "PROVIDER" {
		t.Fatal(err)
	}
}
func TestMalformedConfigNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s := Store{path, []string{"a"}}
	for _, raw := range []string{"{bad", "null", "[]", "{}", `{"version":2,"providers":{}}`, `{"version":1,"providers":[]}`, `{"version":1,"providers":null}`, `{"version":1,"providers":{"a":"false"}}`, `{"version":1,"providers":{"a":null}}`, `{"version":1,"providers":{"unknown":true}}`, `{"version":1,"providers":{},"typo":1}`, `{"version":1,"providers":{}} {}`} {
		t.Run(raw, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []func() error{func() error { _, err := s.Load(); return err }, func() error { return s.SetEnabled("a", false) }} {
				var e *fault.Error
				if err := operation(); !errors.As(err, &e) || e.Code != "CONFIG" {
					t.Fatal(err)
				}
			}
			after, _ := os.ReadFile(path)
			if string(after) != raw {
				t.Fatal("invalid config overwritten")
			}
			entries, _ := os.ReadDir(filepath.Dir(path))
			if len(entries) != 1 {
				t.Fatal("temp residue")
			}
		})
	}
}
func TestFileAccessErrors(t *testing.T) {
	dir := t.TempDir()
	s := Store{dir, []string{"a"}}
	_, err := s.Load()
	if err == nil || !strings.Contains(err.Error(), "Cannot read config") {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Path = filepath.Join(blocker, "config.json")
	if err := s.SetEnabled("a", false); err == nil {
		t.Fatal("write under file succeeded")
	}
}
