// Package recipetin is the sole production provider, using public HTML and Recipe JSON-LD.
package recipetin

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/denwilliams/recipes-cli/internal/fault"
	"github.com/denwilliams/recipes-cli/internal/model"
	"github.com/denwilliams/recipes-cli/internal/netfetch"
	"github.com/denwilliams/recipes-cli/internal/provider"
	"golang.org/x/net/html"
)

const ID = "recipetin-eats"

var hosts = []string{"www.recipetineats.com", "recipetineats.com"}

type Provider struct{}

func (Provider) Info() provider.Info {
	return provider.Info{ID: ID, Name: "RecipeTin Eats", Hosts: append([]string(nil), hosts...), EnabledByDefault: true}
}
func (Provider) Matches(u *url.URL) bool {
	for _, h := range hosts {
		if u.Hostname() == h {
			return true
		}
	}
	return false
}
func (Provider) Search(ctx context.Context, query string, fetch netfetch.Fetch) ([]model.SearchResult, error) {
	u := "https://www.recipetineats.com/?" + url.Values{"s": {query}}.Encode()
	page, err := fetch(ctx, u, hosts)
	if err != nil {
		return nil, err
	}
	return ParseSearch(page.HTML, page.URL)
}
func (Provider) Get(ctx context.Context, input string, fetch netfetch.Fetch) (model.Recipe, error) {
	u, err := netfetch.ValidateURL(input, hosts)
	if err != nil {
		return model.Recipe{}, err
	}
	page, err := fetch(ctx, u.String(), hosts)
	if err != nil {
		return model.Recipe{}, err
	}
	return ParseRecipe(page.HTML, page.URL)
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	walk(n, func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
	})
	return b.String()
}
func normalize(s string) string {
	// Strip terminal controls as well as normalize whitespace; HTML never executes.
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
func text(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	// Tokenize a fragment rather than a whole document (plain strings are valid too).
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind == html.TextToken {
			b.Write(z.Text())
		}
	}
	return normalize(b.String())
}
func list(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{v}
}
func hasType(n map[string]any) bool {
	for _, t := range list(n["@type"]) {
		s, _ := t.(string)
		if s == "Recipe" || s == "https://schema.org/Recipe" || s == "http://schema.org/Recipe" {
			return true
		}
	}
	return false
}
func nodes(v any) []map[string]any {
	out := []map[string]any{}
	switch n := v.(type) {
	case []any:
		for _, item := range n {
			out = append(out, nodes(item)...)
		}
	case map[string]any:
		out = append(out, n)
		out = append(out, nodes(n["@graph"])...)
	}
	return out
}
func steps(v any) []string {
	out := []string{}
	switch n := v.(type) {
	case []any:
		for _, item := range n {
			out = append(out, steps(item)...)
		}
	case string:
		if s := text(n); s != "" {
			out = append(out, s)
		}
	case map[string]any:
		if items, ok := n["itemListElement"]; ok {
			return steps(items)
		}
		s := text(n["text"])
		if s == "" {
			s = text(n["name"])
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func ParseRecipe(input, source string) (model.Recipe, error) {
	root, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return model.Recipe{}, fault.New("RECIPE_DATA", "Cannot parse HTML.")
	}
	scripts := []string{}
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" && strings.EqualFold(attr(n, "type"), "application/ld+json") {
			scripts = append(scripts, nodeText(n))
		}
	})
	malformed := false
	for _, raw := range scripts {
		var data any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			malformed = true
			continue
		}
		for _, n := range nodes(data) {
			if !hasType(n) {
				continue
			}
			title := text(n["name"])
			ingredients := []string{}
			if items, ok := n["recipeIngredient"].([]any); ok {
				for _, v := range items {
					if s := text(v); s != "" {
						ingredients = append(ingredients, s)
					}
				}
			}
			instructions := steps(n["recipeInstructions"])
			if title == "" || len(ingredients) == 0 || len(instructions) == 0 {
				continue
			}
			meta := model.Metadata{Description: text(n["description"]), PrepTime: text(n["prepTime"]), CookTime: text(n["cookTime"]), TotalTime: text(n["totalTime"])}
			authors := []string{}
			for _, v := range list(n["author"]) {
				if m, ok := v.(map[string]any); ok {
					v = m["name"]
				}
				if s := text(v); s != "" {
					authors = append(authors, s)
				}
			}
			meta.Author = strings.Join(authors, ", ")
			for _, v := range list(n["recipeYield"]) {
				s := text(v)
				if number, ok := v.(float64); ok {
					s = strconv.FormatFloat(number, 'f', -1, 64)
				}
				if s != "" {
					meta.Yield = append(meta.Yield, s)
				}
			}
			recipe := model.Recipe{Title: title, SourceURL: source, Provider: ID, Ingredients: ingredients, Instructions: instructions}
			if meta.Author != "" || meta.Description != "" || len(meta.Yield) > 0 || meta.PrepTime != "" || meta.CookTime != "" || meta.TotalTime != "" {
				recipe.Metadata = &meta
			}
			return recipe, nil
		}
	}
	suffix := ""
	if malformed {
		suffix = "; some JSON-LD was malformed"
	}
	return model.Recipe{}, fault.New("RECIPE_DATA", "No complete structured Recipe found (title, ingredients, instructions required)%s. Check original page; no prose/challenge fallback.", suffix)
}
func ParseSearch(input, base string) ([]model.SearchResult, error) {
	root, err := html.Parse(strings.NewReader(input))
	if err != nil && err != io.EOF {
		return nil, err
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	out := []model.SearchResult{}
	seen := map[string]bool{}
	walk(root, func(n *html.Node) {
		if len(out) >= 20 || n.Type != html.ElementNode {
			return
		}
		class := false
		for _, c := range strings.Fields(attr(n, "class")) {
			if c == "entry-title-link" {
				class = true
			}
		}
		if !class {
			return
		}
		article := false
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type == html.ElementNode && p.Data == "article" {
				article = true
				break
			}
		}
		if !article {
			return
		}
		title, href := normalize(nodeText(n)), attr(n, "href")
		if title == "" || href == "" {
			return
		}
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		u, err := netfetch.ValidateURL(baseURL.ResolveReference(ref).String(), hosts)
		if err != nil || seen[u.String()] {
			return
		}
		seen[u.String()] = true
		out = append(out, model.SearchResult{Title: title, SourceURL: u.String(), Provider: ID})
	})
	return out, nil
}
