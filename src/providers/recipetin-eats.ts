import { load } from 'cheerio';
import type { Recipe, SearchResult } from '../model.js';
import type { Provider } from './provider.js';
import { validateUrl } from '../http.js';
import { RecipesError } from '../errors.js';

const id = 'recipetin-eats';
const hosts = ['www.recipetineats.com', 'recipetineats.com'] as const;

function text(value: unknown): string {
  if (typeof value !== 'string') return '';
  // Decode entities and discard markup; output remains plain text, never executable HTML.
  return load(value).text().replace(/\s+/g, ' ').trim();
}
function hasType(node: Record<string, unknown>, type: string): boolean {
  const types = Array.isArray(node['@type']) ? node['@type'] : [node['@type']];
  return types.some((t) => typeof t === 'string' && (t === type || t === `https://schema.org/${type}` || t === `http://schema.org/${type}`));
}
function* nodes(value: unknown): Generator<Record<string, unknown>> {
  if (Array.isArray(value)) { for (const item of value) yield* nodes(item); }
  else if (value && typeof value === 'object') {
    const node = value as Record<string, unknown>;
    yield node;
    if (node['@graph']) yield* nodes(node['@graph']);
  }
}
function steps(value: unknown): string[] {
  if (Array.isArray(value)) return value.flatMap(steps);
  if (typeof value === 'string') return [text(value)].filter(Boolean);
  if (!value || typeof value !== 'object') return [];
  const node = value as Record<string, unknown>;
  if (node.itemListElement) return steps(node.itemListElement);
  return [text(node.text) || text(node.name)].filter(Boolean);
}

export function parseRecipe(html: string, sourceUrl: string): Recipe {
  const $ = load(html);
  let malformed = false;
  for (const element of $('script[type="application/ld+json"]').toArray()) {
    let data: unknown;
    try { data = JSON.parse($(element).text()); }
    catch { malformed = true; continue; }
    for (const node of nodes(data)) {
      if (!hasType(node, 'Recipe')) continue;
      const title = text(node.name);
      const ingredients = Array.isArray(node.recipeIngredient) ? node.recipeIngredient.map(text).filter(Boolean) : [];
      const instructions = steps(node.recipeInstructions);
      if (!title || !ingredients.length || !instructions.length) continue;
      const authors = (Array.isArray(node.author) ? node.author : [node.author]).map((a) =>
        typeof a === 'string' ? text(a) : a && typeof a === 'object' ? text((a as Record<string, unknown>).name) : '',
      ).filter(Boolean).join(', ');
      const metadata: NonNullable<Recipe['metadata']> = {};
      if (authors) metadata.author = authors;
      const description = text(node.description);
      if (description) metadata.description = description;
      if (node.recipeYield) {
        const yields = (Array.isArray(node.recipeYield) ? node.recipeYield : [node.recipeYield]).map((y) =>
          typeof y === 'number' ? String(y) : text(y),
        ).filter(Boolean);
        if (yields.length) metadata.yield = yields;
      }
      for (const key of ['prepTime', 'cookTime', 'totalTime'] as const) {
        const value = text(node[key]);
        if (value) metadata[key] = value;
      }
      return { title, sourceUrl, provider: id, ingredients, instructions, ...(Object.keys(metadata).length ? { metadata } : {}) };
    }
  }
  throw new RecipesError('RECIPE_DATA', `No complete structured Recipe found (title, ingredients and instructions required)${malformed ? '; some JSON-LD was malformed' : ''}. Check the original page; this MVP does not scrape prose or bypass challenges.`);
}

export function parseSearch(html: string, baseUrl: string): SearchResult[] {
  const $ = load(html);
  const seen = new Set<string>();
  const results: SearchResult[] = [];
  for (const element of $('article .entry-title-link').toArray()) {
    const link = $(element);
    const title = link.text().replace(/\s+/g, ' ').trim();
    const href = link.attr('href');
    if (!title || !href) continue;
    let url: URL;
    try { url = validateUrl(new URL(href, baseUrl).href, hosts); }
    catch { continue; }
    if (seen.has(url.href)) continue;
    seen.add(url.href);
    results.push({ title, sourceUrl: url.href, provider: id });
    if (results.length === 20) break;
  }
  return results;
}

export const recipeTinEats: Provider = {
  id, name: 'RecipeTin Eats', hosts, enabledByDefault: true,
  matches: (url) => hosts.some((host) => host === url.hostname),
  async search(query, fetchPage) {
    const url = new URL('https://www.recipetineats.com/');
    url.searchParams.set('s', query);
    const page = await fetchPage(url.href, hosts);
    return parseSearch(page.html, page.url);
  },
  async get(url, fetchPage) {
    const page = await fetchPage(validateUrl(url, hosts).href, hosts);
    return parseRecipe(page.html, page.url);
  },
};
