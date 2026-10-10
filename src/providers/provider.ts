import type { Recipe, SearchResult } from '../model.js';
import type { FetchPage } from '../http.js';

/** Providers own discovery, host matching, parsing and normalized model construction. */
export interface Provider {
  readonly id: string;
  readonly name: string;
  readonly hosts: readonly string[];
  readonly enabledByDefault: boolean;
  matches(url: URL): boolean;
  search(query: string, fetchPage: FetchPage): Promise<SearchResult[]>;
  get(url: string, fetchPage: FetchPage): Promise<Recipe>;
}
