// Package model holds normalized, attributed plain-text recipes, not interpreted quantities.
package model

type Metadata struct {
	Author      string   `json:"author,omitempty"`
	Description string   `json:"description,omitempty"`
	Yield       []string `json:"yield,omitempty"`
	PrepTime    string   `json:"prepTime,omitempty"`
	CookTime    string   `json:"cookTime,omitempty"`
	TotalTime   string   `json:"totalTime,omitempty"`
}
type Recipe struct {
	Title        string    `json:"title"`
	SourceURL    string    `json:"sourceUrl"`
	Provider     string    `json:"provider"`
	Ingredients  []string  `json:"ingredients"`
	Instructions []string  `json:"instructions"`
	Metadata     *Metadata `json:"metadata,omitempty"`
}
type SearchResult struct {
	Title     string `json:"title"`
	SourceURL string `json:"sourceUrl"`
	Provider  string `json:"provider"`
}
