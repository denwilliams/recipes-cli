// Package provider defines the provider boundary and provider-neutral dispatch.
package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/denwilliams/recipes-cli/internal/config"
	"github.com/denwilliams/recipes-cli/internal/fault"
	"github.com/denwilliams/recipes-cli/internal/model"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
)

type Info struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Hosts            []string `json:"-"`
	EnabledByDefault bool     `json:"-"`
}

// Providers own search/parsing/normalized model construction; CLI knows no selectors.
type Provider interface {
	Info() Info
	Matches(*url.URL) bool
	Search(context.Context, string, netfetch.Fetch) ([]model.SearchResult, error)
	Get(context.Context, string, netfetch.Fetch) (model.Recipe, error)
}
type Registry struct{ providers []Provider }

func New(ps ...Provider) (*Registry, error) {
	seen := map[string]bool{}
	for _, p := range ps {
		id := p.Info().ID
		if id == "" || seen[id] {
			return nil, fmt.Errorf("empty/duplicate provider id %q", id)
		}
		seen[id] = true
	}
	return &Registry{append([]Provider(nil), ps...)}, nil
}
func (r *Registry) IDs() []string {
	ids := []string{}
	for _, p := range r.providers {
		ids = append(ids, p.Info().ID)
	}
	return ids
}
func (r *Registry) candidates(selected string) ([]Provider, error) {
	if selected == "" {
		return r.providers, nil
	}
	for _, p := range r.providers {
		if p.Info().ID == selected {
			return []Provider{p}, nil
		}
	}
	return nil, fault.New("PROVIDER", "Unknown provider %q. Available: %s.", selected, strings.Join(r.IDs(), ", "))
}
func enabled(p Provider, cfg config.Config) bool {
	value, ok := cfg.Providers[p.Info().ID]
	if ok {
		return value
	}
	return p.Info().EnabledByDefault
}

type Status struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (r *Registry) Status(cfg config.Config) []Status {
	statuses := []Status{}
	for _, p := range r.providers {
		i := p.Info()
		statuses = append(statuses, Status{i.ID, i.Name, enabled(p, cfg)})
	}
	return statuses
}
func (r *Registry) Search(ctx context.Context, query string, cfg config.Config, fetch netfetch.Fetch, selected string) ([]model.SearchResult, error) {
	if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 200 {
		return nil, fault.New("USAGE", "Search requires a non-empty query of at most 200 characters.")
	}
	ps, err := r.candidates(selected)
	if err != nil {
		return nil, err
	}
	results := []model.SearchResult{}
	active := 0
	for _, p := range ps {
		if !enabled(p, cfg) {
			continue
		}
		active++
		found, err := p.Search(ctx, strings.TrimSpace(query), fetch)
		if err != nil {
			return nil, err
		}
		results = append(results, found...)
	}
	if active == 0 {
		return nil, fault.New("DISABLED", "No selected providers enabled. Run recipes providers enable PROVIDER-ID.")
	}
	if len(results) == 0 {
		return nil, fault.New("EMPTY", "No recipe candidates found. Try another query; site layout changes/challenges may cause empty results.")
	}
	return results, nil
}
func (r *Registry) Get(ctx context.Context, input string, cfg config.Config, fetch netfetch.Fetch, selected string) (model.Recipe, error) {
	ps, err := r.candidates(selected)
	if err != nil {
		return model.Recipe{}, err
	}
	hosts := []string{}
	for _, p := range ps {
		hosts = append(hosts, p.Info().Hosts...)
	}
	u, err := netfetch.ValidateURL(input, hosts)
	if err != nil {
		return model.Recipe{}, err
	}
	for _, p := range ps {
		if !p.Matches(u) {
			continue
		}
		if !enabled(p, cfg) {
			return model.Recipe{}, fault.New("DISABLED", "Provider %q disabled. Run recipes providers enable %s.", p.Info().ID, p.Info().ID)
		}
		return p.Get(ctx, u.String(), fetch)
	}
	return model.Recipe{}, fault.New("URL", "No provider supports this URL.")
}
