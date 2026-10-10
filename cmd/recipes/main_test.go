package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Builds/copies the real native executable and runs it with no PATH/runtime.
// Only offline commands: unsafe/disabled paths must not reach DNS or transport.
func TestNativeInstalledExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "recipes")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("native build: %s %v", b, err)
	}
	path := filepath.Join(dir, "config.json")
	run := func(args ...string) (int, string, string) {
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"PATH=" + filepath.Join(dir, "empty"), "HOME=" + dir, "RECIPES_CONFIG=" + path, "HTTPS_PROXY=http://127.0.0.1:9"}
		var out, errOut bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errOut
		err := cmd.Run()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return code, out.String(), errOut.String()
	}
	for _, args := range [][]string{{"--help"}, {"--version"}, {"providers", "status", "--json"}, {"providers", "disable", "recipetin-eats"}} {
		code, out, err := run(args...)
		if code != 0 || out == "" || err != "" {
			t.Fatal(code, out, err)
		}
	}
	code, out, err := run("providers", "status", "--json")
	var status struct{ Providers []struct{ Enabled bool } }
	if code != 0 || json.Unmarshal([]byte(out), &status) != nil || status.Providers[0].Enabled || err != "" {
		t.Fatal(code, out, err)
	}
	for _, args := range [][]string{{"search", "chicken"}, {"search", "chicken", "--provider", "recipetin-eats"}, {"get", "https://www.recipetineats.com/butter-chicken/"}, {"get", "https://www.recipetineats.com/butter-chicken/", "--provider", "recipetin-eats"}} {
		code, out, err := run(args...)
		if code != 1 || out != "" || !strings.Contains(err, "DISABLED") {
			t.Fatal(code, out, err)
		}
	}
	code, out, err = run("providers", "enable", "recipetin-eats")
	if code != 0 || out == "" || err != "" {
		t.Fatal(code, out, err)
	}
	code, out, err = run("get", "https://evil.example/")
	if code != 1 || out != "" || !strings.Contains(err, "URL") {
		t.Fatal(code, out, err)
	}
	code, out, err = run("search")
	if code != 2 || out != "" || !strings.Contains(err, "USAGE") {
		t.Fatal(code, out, err)
	}
	before := []byte("{broken")
	os.WriteFile(path, before, 0600)
	code, out, err = run("providers", "disable", "recipetin-eats")
	if code != 1 || out != "" || !strings.Contains(err, "CONFIG") {
		t.Fatal(code, out, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("malformed config overwritten")
	}
}

func TestReleaseVersion(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "recipes")
	const version = "v1.2.3"
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-X=github.com/denwilliams/recipes-cli/internal/cli.Version="+version, "-o", bin, ".")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("release build: %s %v", b, err)
	}
	cmd := exec.Command(bin, "--version")
	cmd.Env = append(os.Environ(), "PATH=", "RECIPES_CONFIG="+filepath.Join(t.TempDir(), "missing"))
	if b, err := cmd.CombinedOutput(); err != nil || string(b) != version+"\n" {
		t.Fatalf("release version: %q %v", b, err)
	}
}
