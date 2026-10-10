package provider

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/denwilliams/recipes-cli/internal/config"
	"github.com/denwilliams/recipes-cli/internal/fault"
	"github.com/denwilliams/recipes-cli/internal/model"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
)

type synthetic struct {
	id                    string
	calls                 *[]string
	defaultEnabled, empty bool
}

func (p synthetic) Info() Info {
	return Info{ID: p.id, Name: p.id, Hosts: []string{p.id + ".example"}, EnabledByDefault: p.defaultEnabled}
}
func (p synthetic) Matches(u *url.URL) bool { return u.Hostname() == p.id+".example" }
func (p synthetic) Search(context.Context, string, netfetch.Fetch) ([]model.SearchResult, error) {
	*p.calls = append(*p.calls, p.id+":search")
	if p.empty {
		return nil, nil
	}
	return []model.SearchResult{{Title: p.id, SourceURL: "https://" + p.id + ".example/", Provider: p.id}}, nil
}
func (p synthetic) Get(_ context.Context, input string, _ netfetch.Fetch) (model.Recipe, error) {
	*p.calls = append(*p.calls, p.id+":get")
	return model.Recipe{Title: p.id, SourceURL: input, Provider: p.id, Ingredients: []string{"salt"}, Instructions: []string{"Use"}}, nil
}
func noFetch(context.Context, string, []string) (netfetch.Page, error) {
	panic("network must not be invoked")
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var e *fault.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
func TestMultipleProvidersAndDefaults(t *testing.T) {
	calls := []string{}
	a, b := synthetic{"a", &calls, true, false}, synthetic{"b", &calls, false, false}
	r, err := New(a, b)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	status := r.Status(cfg)
	if !status[0].Enabled || status[1].Enabled {
		t.Fatal(status)
	}
	cfg.Providers["b"] = true
	found, err := r.Search(context.Background(), "salt", cfg, noFetch, "")
	if err != nil || len(found) != 2 {
		t.Fatal(found, err)
	}
	cfg.Providers["a"] = false
	found, err = r.Search(context.Background(), "salt", cfg, noFetch, "")
	if err != nil || len(found) != 1 || found[0].Provider != "b" {
		t.Fatal(found, err)
	}
	_, err = r.Search(context.Background(), "salt", config.Default(), noFetch, "a")
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := r.Get(context.Background(), "https://b.example/recipe/", cfg, noFetch, "")
	if err != nil || recipe.Provider != "b" {
		t.Fatal(recipe, err)
	}
	if !reflect.DeepEqual(calls, []string{"a:search", "b:search", "b:search", "a:search", "b:get"}) {
		t.Fatal(calls)
	}
	if _, err := New(a, a); err == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestDisabledStopsBeforeProviderOrFetch(t *testing.T) {
	calls := []string{}
	r, _ := New(synthetic{"a", &calls, true, false})
	cfg := config.Default()
	cfg.Providers["a"] = false
	for _, selected := range []string{"", "a"} {
		_, err := r.Search(context.Background(), "salt", cfg, noFetch, selected)
		code(t, err, "DISABLED")
		_, err = r.Get(context.Background(), "https://a.example/", cfg, noFetch, selected)
		code(t, err, "DISABLED")
	}
	if len(calls) != 0 {
		t.Fatal("disabled provider invoked", calls)
	}
}
func TestDispatchErrors(t *testing.T) {
	calls := []string{}
	r, _ := New(synthetic{"a", &calls, true, true})
	cfg := config.Default()
	_, err := r.Search(context.Background(), "x", cfg, noFetch, "missing")
	code(t, err, "PROVIDER")
	_, err = r.Get(context.Background(), "https://a.example/", cfg, noFetch, "missing")
	code(t, err, "PROVIDER")
	for _, input := range []string{"bad", "http://a.example/", "https://other.example/", "https://u:p@a.example/", "https://a.example:444/"} {
		_, err := r.Get(context.Background(), input, cfg, noFetch, "")
		code(t, err, "URL")
	}
	for _, q := range []string{" ", strings.Repeat("x", 201)} {
		_, err := r.Search(context.Background(), q, cfg, noFetch, "")
		code(t, err, "USAGE")
	}
	if len(calls) != 0 {
		t.Fatal(calls)
	}
	_, err = r.Search(context.Background(), "salt", cfg, noFetch, "")
	code(t, err, "EMPTY")
}
