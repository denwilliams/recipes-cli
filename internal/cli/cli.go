// Package cli provides provider-neutral CLI handlers and injectable offline dependencies.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/denwilliams/recipes-cli/internal/config"
	"github.com/denwilliams/recipes-cli/internal/fault"
	"github.com/denwilliams/recipes-cli/internal/model"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
	"github.com/denwilliams/recipes-cli/internal/provider"
	"github.com/denwilliams/recipes-cli/internal/recipetin"
)

const Help = `Recipes CLI — attributed recipes from configurable providers

Usage:
  recipes search QUERY [--provider ID] [--json]
  recipes get HTTPS_URL [--provider ID] [--json]
  recipes providers status [--json]
  recipes providers enable ID
  recipes providers disable ID

Options (before or after positionals):
  --config FILE   Overrides RECIPES_CONFIG, then XDG_CONFIG_HOME
  --provider ID  Select an enabled provider
  --json         Machine-readable output
  --help, -h     Show help
  --version      Show version
  --             End option parsing

Search reads one page, up to 20 candidate posts per provider, not all recipes.
`

type Dependencies struct {
	Registry *provider.Registry
	Fetch    netfetch.Fetch
	Env      func(string) (string, bool)
	Out, Err io.Writer
}
type options struct {
	config, provider    *string
	json, help, version bool
	args                []string
}

func parse(args []string) (o options, err error) {
	positional := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if positional || !strings.HasPrefix(a, "-") || a == "-" {
			o.args = append(o.args, a)
			continue
		}
		if a == "--" {
			positional = true
			continue
		}
		key, value, assigned := strings.Cut(a, "=")
		switch key {
		case "--config", "--provider":
			if !assigned {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "--") {
					return o, fault.New("USAGE", "%s requires a value. Run recipes --help.", key)
				}
				value = args[i]
			}
			if key == "--config" {
				v := value
				o.config = &v
			} else {
				v := value
				o.provider = &v
			}
		case "--json", "--help", "-h", "--version":
			if assigned {
				return o, fault.New("USAGE", "%s does not take a value.", key)
			}
			switch key {
			case "--json":
				o.json = true
			case "--help", "-h":
				o.help = true
			case "--version":
				o.version = true
			}
		default:
			return o, fault.New("USAGE", "Unknown option %q. Run recipes --help.", a)
		}
	}
	return o, nil
}
func Run(ctx context.Context, args []string, d Dependencies) int {
	if d.Out == nil {
		d.Out = os.Stdout
	}
	if d.Err == nil {
		d.Err = os.Stderr
	}
	if d.Env == nil {
		d.Env = os.LookupEnv
	}
	if d.Registry == nil {
		d.Registry, _ = provider.New(recipetin.Provider{})
	}
	if d.Fetch == nil {
		d.Fetch = netfetch.New(netfetch.Options{})
	}
	err := execute(ctx, args, d)
	if err == nil {
		return 0
	}
	var e *fault.Error
	if !errors.As(err, &e) {
		e = &fault.Error{Code: "INTERNAL", Message: "Unexpected failure. Report command at https://github.com/denwilliams/recipes-cli/issues."}
	}
	fmt.Fprintf(d.Err, "Error [%s]: %s\n", e.Code, e.Message)
	if e.Code == "USAGE" {
		return 2
	}
	return 1
}
func output(w io.Writer, data any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	e.SetEscapeHTML(false)
	return e.Encode(data)
}
func execute(ctx context.Context, args []string, d Dependencies) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	if o.help || len(args) == 0 {
		_, err = io.WriteString(d.Out, Help)
		return err
	}
	if o.version {
		_, err = io.WriteString(d.Out, "0.1.0-go\n")
		return err
	}
	if o.provider != nil && strings.TrimSpace(*o.provider) == "" {
		return fault.New("USAGE", "--provider requires a non-empty provider ID.")
	}
	if len(o.args) == 0 {
		return fault.New("USAGE", "Missing command. Run recipes --help.")
	}
	command, rest := o.args[0], o.args[1:]
	selected := ""
	if o.provider != nil {
		selected = *o.provider
	}
	switch command {
	case "search":
		if len(rest) == 0 {
			return fault.New("USAGE", "recipes search requires a query.")
		}
	case "get":
		if len(rest) != 1 {
			return fault.New("USAGE", "recipes get requires one HTTPS URL.")
		}
	case "providers":
		valid := len(rest) == 1 && rest[0] == "status" || len(rest) == 2 && (rest[0] == "enable" || rest[0] == "disable") && !o.json
		if !valid || o.provider != nil {
			return fault.New("USAGE", "Use recipes providers status [--json], enable ID or disable ID.")
		}
	default:
		return fault.New("USAGE", "Unknown command. Run recipes --help.")
	}
	path, err := config.Path(o.config, d.Env)
	if err != nil {
		return err
	}
	store := config.Store{Path: path, IDs: d.Registry.IDs()}
	cfg, err := store.Load()
	if err != nil {
		return err
	}
	switch command {
	case "providers":
		if rest[0] != "status" {
			if err := store.SetEnabled(rest[1], rest[0] == "enable"); err != nil {
				return err
			}
			state := "disabled"
			if rest[0] == "enable" {
				state = "enabled"
			}
			_, err = fmt.Fprintf(d.Out, "%s %s. Config: %s\n", rest[1], state, path)
			return err
		}
		statuses := d.Registry.Status(cfg)
		if o.json {
			return output(d.Out, struct {
				ConfigPath string            `json:"configPath"`
				Providers  []provider.Status `json:"providers"`
			}{path, statuses})
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Config: %s\n", path)
		for _, p := range statuses {
			state := "disabled"
			if p.Enabled {
				state = "enabled"
			}
			fmt.Fprintf(&b, "%s: %s (%s)\n", p.ID, state, p.Name)
		}
		_, err = io.WriteString(d.Out, b.String())
		return err
	case "search":
		results, err := d.Registry.Search(ctx, strings.Join(rest, " "), cfg, d.Fetch, selected)
		if err != nil {
			return err
		}
		if o.json {
			return output(d.Out, results)
		}
		var b strings.Builder
		for _, r := range results {
			fmt.Fprintf(&b, "%s\n  %s (%s)\n", r.Title, r.SourceURL, r.Provider)
		}
		_, err = io.WriteString(d.Out, b.String())
		return err
	case "get":
		recipe, err := d.Registry.Get(ctx, rest[0], cfg, d.Fetch, selected)
		if err != nil {
			return err
		}
		if o.json {
			return output(d.Out, recipe)
		}
		_, err = io.WriteString(d.Out, recipeText(recipe))
		return err
	}
	return nil
}
func recipeText(r model.Recipe) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nSource: %s (%s)\n", r.Title, r.SourceURL, r.Provider)
	if r.Metadata != nil && r.Metadata.Author != "" {
		fmt.Fprintf(&b, "Author: %s\n", r.Metadata.Author)
	}
	b.WriteString("\nIngredients:\n")
	for _, ingredient := range r.Ingredients {
		fmt.Fprintf(&b, "- %s\n", ingredient)
	}
	b.WriteString("\nInstructions:\n")
	for i, step := range r.Instructions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, step)
	}
	b.WriteString("\nSee source for notes, substitutions and safety/allergen guidance.\n")
	return b.String()
}
