// Opt-in: exactly one public search and one recipe request, production safeguards.
// Never invoked by tests/CI; no user config, fixture updates or retries.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/denwilliams/recipes-cli/internal/config"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
	"github.com/denwilliams/recipes-cli/internal/provider"
	"github.com/denwilliams/recipes-cli/internal/recipetin"
)

func main() {
	registry, _ := provider.New(recipetin.Provider{})
	fetch := netfetch.New(netfetch.Options{})
	results, err := registry.Search(context.Background(), "chicken", config.Default(), fetch, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Search:", err)
		os.Exit(1)
	}
	fmt.Printf("Search: %d attributed candidates\n", len(results))
	recipe, err := registry.Get(context.Background(), "https://www.recipetineats.com/butter-chicken/", config.Default(), fetch, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Recipe:", err)
		os.Exit(1)
	}
	fmt.Printf("Recipe: %s, %d ingredients, %d steps\nSource: %s (%s)\n", recipe.Title, len(recipe.Ingredients), len(recipe.Instructions), recipe.SourceURL, recipe.Provider)
}
