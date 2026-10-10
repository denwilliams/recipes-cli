/** Text is normalized, not scaled or interpreted. Source attribution is mandatory. */
export interface Recipe {
  title: string;
  sourceUrl: string;
  provider: string;
  ingredients: string[];
  instructions: string[];
  metadata?: {
    author?: string;
    description?: string;
    yield?: string[];
    prepTime?: string;
    cookTime?: string;
    totalTime?: string;
  };
}
export interface SearchResult {
  title: string;
  sourceUrl: string;
  provider: string;
}
