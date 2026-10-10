// Explicitly opt-in: exactly one search + one recipe fetch (excluding validated redirects).
// No pagination, retries, fixture updates or bypasses. Independent of offline tests.
import assert from 'node:assert/strict';
import { createFetcher } from '../dist/src/http.js';
import { registry } from '../dist/src/providers/index.js';
const config = { version: 1, providers: {} };
const fetchPage = createFetcher();
const results = await registry.search('chicken', config, fetchPage);
assert.ok(results.length > 0 && results.length <= 20);
console.log(`Search: ${results.length} attributed candidate posts.`);
const recipe = await registry.get('https://www.recipetineats.com/butter-chicken/', config, fetchPage);
assert.equal(recipe.title, 'Butter Chicken');
assert.ok(recipe.ingredients.length > 0 && recipe.instructions.length > 0);
console.log(`Get: ${recipe.title}; ${recipe.ingredients.length} ingredients, ${recipe.instructions.length} steps.`);
console.log(`Source: ${recipe.sourceUrl} (${recipe.provider})`);
