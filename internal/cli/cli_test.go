package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denwilliams/recipes-cli/internal/fault"
	"github.com/denwilliams/recipes-cli/internal/model"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
	"github.com/denwilliams/recipes-cli/internal/provider"
)

const testURL = "https://www.recipetineats.com/butter-chicken/"

type result struct {
	exit     int
	out, err string
}

func invoke(args []string, d Dependencies) result {
	var out, err bytes.Buffer
	d.Out = &out
	d.Err = &err
	return result{Run(context.Background(), args, d), out.String(), err.String()}
}
func setup(t *testing.T) (string, Dependencies, *int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	calls := 0
	d := Dependencies{Env: func(k string) (string, bool) {
		if k == "RECIPES_CONFIG" {
			return path, true
		}
		return "", false
	}, Fetch: func(_ context.Context, input string, _ []string) (netfetch.Page, error) {
		calls++
		u, _ := url.Parse(input)
		name := "butter-chicken.html"
		if u.Query().Has("s") {
			name = "search-chicken.html"
		}
		b, err := os.ReadFile("../../testdata/" + name)
		return netfetch.Page{HTML: string(b), URL: input}, err
	}}
	return path, d, &calls
}
func TestHelpVersionUsageNoIO(t *testing.T) {
	path, d, calls := setup(t)
	os.WriteFile(path, []byte("bad"), 0600)
	for _, args := range [][]string{nil, {"--help"}, {"-h"}} {
		r := invoke(args, d)
		if r.exit != 0 || !strings.Contains(r.out, "Usage:") || r.err != "" {
			t.Fatal(r)
		}
	}
	if r := invoke([]string{"--version"}, d); r.exit != 0 || r.out != "0.1.0-go\n" {
		t.Fatal(r)
	}
	for _, args := range [][]string{{"unknown"}, {"search"}, {"get"}, {"get", "one", "two"}, {"--not-an-option"}, {"--json=true"}, {"--config"}, {"--provider"}, {"providers"}, {"providers", "status", "extra"}, {"providers", "enable"}, {"providers", "disable", "recipetin-eats", "--json"}, {"providers", "status", "--provider", "recipetin-eats"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := invoke(args, d)
			if r.exit != 2 || r.out != "" || !strings.Contains(r.err, "USAGE") {
				t.Fatal(r)
			}
		})
	}
	if *calls != 0 {
		t.Fatal("network reached")
	}
}
func TestCLITextJSONAndInterspersedOptions(t *testing.T) {
	_, d, calls := setup(t)
	r := invoke([]string{"search", "chicken", "--json"}, d)
	var found []model.SearchResult
	if r.exit != 0 || json.Unmarshal([]byte(r.out), &found) != nil || len(found) != 2 || found[0].Provider != "recipetin-eats" {
		t.Fatal(r)
	}
	r = invoke([]string{"search", "chicken", "thigh", "--provider=recipetin-eats"}, d)
	if r.exit != 0 || !strings.Contains(r.out, "Oven Baked Chicken Schnitzel\n  https:") {
		t.Fatal(r)
	}
	r = invoke([]string{"--json", "get", testURL}, d)
	var recipe model.Recipe
	if r.exit != 0 || json.Unmarshal([]byte(r.out), &recipe) != nil || recipe.SourceURL != testURL || len(recipe.Instructions) != 7 {
		t.Fatal(r)
	}
	r = invoke([]string{"get", testURL}, d)
	for _, s := range []string{"Source: " + testURL, "Ingredients:\n- 1/2 cup", "Instructions:\n1.", "See source for notes"} {
		if !strings.Contains(r.out, s) {
			t.Fatal(r)
		}
	}
	r = invoke([]string{"search", "--", "-chicken"}, d)
	if r.exit != 0 {
		t.Fatal(r)
	}
	if *calls != 5 {
		t.Fatal(*calls)
	}
}
func TestPersistedDisableNoNetworkAndRestore(t *testing.T) {
	path, d, calls := setup(t)
	r := invoke([]string{"providers", "status", "--json"}, d)
	var status struct {
		ConfigPath string
		Providers  []provider.Status
	}
	if r.exit != 0 || json.Unmarshal([]byte(r.out), &status) != nil || !status.Providers[0].Enabled || status.ConfigPath != path {
		t.Fatal(r)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("status created config")
	}
	r = invoke([]string{"providers", "disable", "recipetin-eats"}, d)
	if r.exit != 0 {
		t.Fatal(r)
	}
	r = invoke([]string{"providers", "status"}, d)
	if !strings.Contains(r.out, "recipetin-eats: disabled") {
		t.Fatal(r)
	}
	for _, args := range [][]string{{"search", "chicken"}, {"search", "chicken", "--provider", "recipetin-eats"}, {"get", testURL}, {"get", testURL, "--provider", "recipetin-eats"}} {
		r := invoke(args, d)
		if r.exit != 1 || r.out != "" || !strings.Contains(r.err, "DISABLED") {
			t.Fatal(r)
		}
	}
	if *calls != 0 {
		t.Fatal("disabled provider fetched")
	}
	r = invoke([]string{"providers", "enable", "recipetin-eats"}, d)
	if r.exit != 0 {
		t.Fatal(r)
	}
	r = invoke([]string{"search", "chicken"}, d)
	if r.exit != 0 || *calls != 1 {
		t.Fatal(r)
	}
}
func TestActionableCLIErrors(t *testing.T) {
	path, d, calls := setup(t)
	for _, args := range [][]string{{"search", "chicken", "--provider", "missing"}, {"providers", "enable", "missing"}, {"providers", "disable", "missing"}, {"get", "https://evil.example/"}} {
		r := invoke(args, d)
		if r.exit != 1 || r.out != "" || (!strings.Contains(r.err, "PROVIDER") && !strings.Contains(r.err, "URL")) {
			t.Fatal(r)
		}
	}
	for _, args := range [][]string{{"search", " "}, {"search", "chicken", "--provider", ""}, {"search", strings.Repeat("x", 201)}} {
		r := invoke(args, d)
		if r.exit != 2 || r.out != "" {
			t.Fatal(r)
		}
	}
	if *calls != 0 {
		t.Fatal(*calls)
	}
	for _, c := range []string{"HTTP", "TIMEOUT", "SIZE", "NETWORK"} {
		d2 := d
		d2.Fetch = func(context.Context, string, []string) (netfetch.Page, error) {
			return netfetch.Page{}, fault.New(c, "Try later")
		}
		r := invoke([]string{"search", "x"}, d2)
		if r.exit != 1 || r.out != "" || !strings.Contains(r.err, c) {
			t.Fatal(r)
		}
	}
	empty := d
	empty.Fetch = func(_ context.Context, u string, _ []string) (netfetch.Page, error) {
		return netfetch.Page{HTML: "No results", URL: u}, nil
	}
	for _, test := range []struct {
		args []string
		code string
	}{{[]string{"search", "x"}, "EMPTY"}, {[]string{"get", testURL}, "RECIPE_DATA"}} {
		r := invoke(test.args, empty)
		if r.exit != 1 || r.out != "" || !strings.Contains(r.err, test.code) {
			t.Fatal(r)
		}
	}
	os.WriteFile(path, []byte("{invalid"), 0600)
	r := invoke([]string{"search", "chicken"}, d)
	if r.exit != 1 || r.out != "" || !strings.Contains(r.err, "CONFIG") {
		t.Fatal(r)
	}
}
func TestExplicitConfigOverridesEnvAndMalformedIsUnchanged(t *testing.T) {
	path, d, _ := setup(t)
	os.WriteFile(path, []byte("bad JSON"), 0600)
	other := path + ".other"
	r := invoke([]string{"providers", "disable", "recipetin-eats", "--config", other}, d)
	if r.exit != 0 {
		t.Fatal(r)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "bad JSON" {
		t.Fatal("env config changed")
	}
	b, _ = os.ReadFile(other)
	if !strings.Contains(string(b), `"recipetin-eats": false`) {
		t.Fatal(string(b))
	}
}

type second struct {
	id    string
	calls *int
	on    bool
}

func (p second) Info() provider.Info {
	return provider.Info{ID: p.id, Name: p.id, Hosts: []string{p.id + ".example"}, EnabledByDefault: p.on}
}
func (p second) Matches(u *url.URL) bool { return u.Hostname() == p.id+".example" }
func (p second) Search(context.Context, string, netfetch.Fetch) ([]model.SearchResult, error) {
	(*p.calls)++
	return []model.SearchResult{{Title: "Salt", SourceURL: "https://" + p.id + ".example/salt/", Provider: p.id}}, nil
}
func (p second) Get(_ context.Context, u string, _ netfetch.Fetch) (model.Recipe, error) {
	(*p.calls)++
	return model.Recipe{Title: "Salt", SourceURL: u, Provider: p.id, Ingredients: []string{"Salt"}, Instructions: []string{"Use"}}, nil
}
func TestSecondProviderUnchangedCLIHandlers(t *testing.T) {
	_, d, network := setup(t)
	calls := 0
	d.Registry, _ = provider.New(second{"one", &calls, true}, second{"two", &calls, false})
	for _, args := range [][]string{{"providers", "enable", "two"}, {"search", "salt", "--provider", "two", "--json"}, {"get", "https://two.example/salt/"}, {"search", "salt", "--json"}, {"providers", "disable", "one"}, {"search", "salt", "--json"}} {
		r := invoke(args, d)
		if r.exit != 0 {
			t.Fatal(args, r)
		}
	}
	if calls != 5 || *network != 0 {
		t.Fatalf("provider calls%d network%d", calls, *network)
	}
	r := invoke([]string{"get", "https://one.example/"}, d)
	if r.exit != 1 || !strings.Contains(r.err, "DISABLED") {
		t.Fatal(r)
	}
	r = invoke([]string{"get", "https://one.example/", "--provider", "two"}, d)
	if r.exit != 1 || !strings.Contains(r.err, "URL") {
		t.Fatal(r)
	}
}
func TestOutputWriteFailure(t *testing.T) {
	_, d, _ := setup(t)
	d.Out = badWriter{}
	var err bytes.Buffer
	d.Err = &err
	if code := Run(context.Background(), []string{"--help"}, d); code != 1 || !strings.Contains(err.String(), "INTERNAL") {
		t.Fatal(code, err.String())
	}
}

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("write failed") }
