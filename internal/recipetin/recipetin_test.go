package recipetin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/denwilliams/recipes-cli/internal/fault"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
)

const source = "https://www.recipetineats.com/butter-chicken/"

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func ld(data any) string {
	b, _ := json.Marshal(data)
	return `<script type="application/ld+json">` + string(b) + `</script>`
}
func baseRecipe() map[string]any {
	return map[string]any{"@type": "Recipe", "name": "A &amp; B", "recipeIngredient": []any{"1 cup <b>rice</b>"}, "recipeInstructions": []any{"Cook &nbsp;well."}}
}
func TestReducedRealRecipe(t *testing.T) {
	r, err := ParseRecipe(fixture(t, "butter-chicken.html"), source)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Butter Chicken" || r.SourceURL != source || r.Provider != ID || len(r.Ingredients) != 17 || len(r.Instructions) != 7 {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(r.Instructions[2], "Cook chicken: Heat") || strings.Contains(strings.Join(r.Instructions, ""), "&nbsp;") {
		t.Fatal(r.Instructions)
	}
	if r.Metadata == nil || r.Metadata.Author != "Nagi | RecipeTin Eats" || r.Metadata.TotalTime != "PT35M" || !reflect.DeepEqual(r.Metadata.Yield, []string{"3", "3 - 4"}) || !strings.Contains(r.Metadata.Description, "you'll") {
		t.Fatalf("%+v", r.Metadata)
	}
}
func TestJSONLDVariantsAndOrderedSections(t *testing.T) {
	n := baseRecipe()
	n["@type"] = []any{"Thing", "https://schema.org/Recipe"}
	n["recipeInstructions"] = []any{map[string]any{"@type": "HowToSection", "name": "Sauce", "itemListElement": []any{map[string]any{"@type": "HowToStep", "text": "<p>Stir &amp; serve.</p>", "url": "http://127.0.0.1/"}}}, map[string]any{"name": "Cool."}, "Eat."}
	n["url"] = "https://evil.example/canonical"
	for _, data := range []any{n, []any{map[string]any{"@type": "WebPage"}, n}, map[string]any{"@graph": []any{n}}} {
		r, err := ParseRecipe(ld(data), source)
		if err != nil {
			t.Fatal(err)
		}
		if r.Title != "A & B" || !reflect.DeepEqual(r.Ingredients, []string{"1 cup rice"}) || !reflect.DeepEqual(r.Instructions, []string{"Stir & serve.", "Cool.", "Eat."}) || r.Metadata != nil || r.SourceURL != source {
			t.Fatalf("%+v", r)
		}
	}
}
func TestSkipMalformedAndIncompleteNodes(t *testing.T) {
	incomplete := baseRecipe()
	incomplete["recipeIngredient"] = []any{}
	r, err := ParseRecipe(`<script type="application/ld+json">{bad</script>`+ld(map[string]any{"@graph": []any{incomplete, baseRecipe()}}), source)
	if err != nil || r.Title != "A & B" {
		t.Fatal(r, err)
	}
}
func TestMissingRecipeData(t *testing.T) {
	cases := []string{"<p>No recipe</p>", `<script type="application/ld+json">{oops</script>`, ld(map[string]any{"@type": "Article"})}
	for _, field := range []string{"name", "recipeIngredient", "recipeInstructions"} {
		n := baseRecipe()
		n[field] = nil
		cases = append(cases, ld(n))
	}
	for i, input := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, err := ParseRecipe(input, source)
			var e *fault.Error
			if !errors.As(err, &e) || e.Code != "RECIPE_DATA" {
				t.Fatal(err)
			}
		})
	}
}
func TestMetadataAndPlainText(t *testing.T) {
	n := baseRecipe()
	n["@type"] = "http://schema.org/Recipe"
	n["author"] = []any{"A &amp; B", map[string]any{"name": "C"}}
	n["recipeYield"] = []any{4, "4 servings"}
	n["description"] = "<p>Rice</p>"
	n["prepTime"] = "PT5M"
	n["name"] = "A\x1b &amp; B"
	r, err := ParseRecipe(ld(n), source)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "A & B" || r.Metadata.Author != "A & B, C" || !reflect.DeepEqual(r.Metadata.Yield, []string{"4", "4 servings"}) || r.Metadata.Description != "Rice" || r.Metadata.PrepTime != "PT5M" {
		t.Fatalf("%+v %+v", r, r.Metadata)
	}
}
func TestReducedRealSearch(t *testing.T) {
	results, err := ParseSearch(fixture(t, "search-chicken.html"), source)
	if err != nil || len(results) != 2 {
		t.Fatal(results, err)
	}
	if results[0].Title != "Oven Baked Chicken Schnitzel" || results[0].SourceURL != "https://www.recipetineats.com/oven-baked-chicken-schnitzel/" || results[0].Provider != ID {
		t.Fatal(results)
	}
}
func TestSearchFilteringAndCap(t *testing.T) {
	link := func(href, title string) string {
		return fmt.Sprintf(`<article><a class="entry-title-link" href="%s">%s</a></article>`, href, title)
	}
	input := `<a class="entry-title-link" href="/navigation/">Nav</a>` + link("/rice/#recipe", "Salt &amp; Pepper") + link("/rice/", "Duplicate") + link("https://evil.example/", "Unsafe") + link("http://www.recipetineats.com/", "Unsafe") + link("https://me:pw@www.recipetineats.com/", "Unsafe") + link("/empty/", "")
	for i := 0; i < 30; i++ {
		input += link(fmt.Sprintf("/recipe-%d/", i), "Food")
	}
	results, err := ParseSearch(input, source)
	if err != nil || len(results) != 20 || results[0].Title != "Salt & Pepper" || results[0].SourceURL != "https://www.recipetineats.com/rice/" {
		t.Fatal(results, err)
	}
	empty, err := ParseSearch("No results", source)
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}
func TestProviderQueryAndFinalAttribution(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, input string, hosts []string) (netfetch.Page, error) {
		calls++
		u, _ := url.Parse(input)
		if calls == 1 && u.Query().Get("s") != "salt & pepper" {
			t.Fatal(input)
		}
		if !reflect.DeepEqual(hosts, []string{"www.recipetineats.com", "recipetineats.com"}) {
			t.Fatal(hosts)
		}
		return netfetch.Page{HTML: ld(baseRecipe()), URL: source}, nil
	}
	p := Provider{}
	_, err := p.Search(context.Background(), "salt & pepper", fetch)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Get(context.Background(), "https://www.recipetineats.com/old/", fetch)
	if err != nil || r.SourceURL != source || calls != 2 {
		t.Fatal(r, err)
	}
	_, err = p.Get(context.Background(), "https://evil.example/", fetch)
	if err == nil || calls != 2 {
		t.Fatal("unsafe URL fetched")
	}
}
