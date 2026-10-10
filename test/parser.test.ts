import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { parseRecipe, parseSearch, recipeTinEats } from '../src/providers/recipetin-eats.js';
import { RecipesError } from '../src/errors.js';
const url = 'https://www.recipetineats.com/butter-chicken/';
const ld = (data: unknown) => `<script type="application/ld+json">${JSON.stringify(data)}</script>`;
const base = { '@type': 'Recipe', name: 'A &amp; B', recipeIngredient: ['1 cup <b>rice</b>'], recipeInstructions: ['Cook &nbsp;well.'] };

test('real reduced RecipeTin Eats JSON-LD graph normalizes attribution, ingredients, steps and metadata', async () => {
  const recipe = parseRecipe(await readFile('test/fixtures/butter-chicken.html', 'utf8'), url);
  assert.equal(recipe.title, 'Butter Chicken');
  assert.equal(recipe.sourceUrl, url);
  assert.equal(recipe.provider, 'recipetin-eats');
  assert.equal(recipe.ingredients.length, 17);
  assert.equal(recipe.instructions.length, 7);
  assert.match(recipe.instructions[2]!, /^Cook chicken: Heat/);
  assert.ok(!recipe.instructions.join('').includes('&nbsp;'));
  assert.equal(recipe.metadata?.author, 'Nagi | RecipeTin Eats');
  assert.deepEqual(recipe.metadata?.yield, ['3', '3 - 4']);
  assert.equal(recipe.metadata?.totalTime, 'PT35M');
  assert.match(recipe.metadata!.description!, /you'll/);
});
test('standalone Recipe, root arrays, typed arrays and nested sections', () => {
  const recipe = parseRecipe(ld([{ '@type': 'WebPage' }, { ...base, '@type': ['Thing', 'https://schema.org/Recipe'], recipeInstructions: [
    { '@type': 'HowToSection', name: 'Sauce', itemListElement: [{ '@type': 'HowToStep', text: '<p>Stir &amp; serve.</p>' }] },
    { '@type': 'HowToStep', name: 'Cool.' }, 'Eat.',
  ] }]), url);
  assert.equal(recipe.title, 'A & B');
  assert.deepEqual(recipe.ingredients, ['1 cup rice']);
  assert.deepEqual(recipe.instructions, ['Stir & serve.', 'Cool.', 'Eat.']);
  assert.equal(recipe.metadata, undefined);
  assert.equal(parseRecipe(ld(base), url).title, 'A & B');
});
test('skip unrelated/malformed/incomplete nodes but accept later complete recipe', () => {
  const html = '<script type="application/ld+json">{bad</script>' + ld({ '@graph': [{ ...base, recipeIngredient: [] }, base] });
  assert.equal(parseRecipe(html, url).title, 'A & B');
});
for (const [name, html] of [
  ['absent', '<p>No recipe</p>'], ['malformed', '<script type="application/ld+json">{oops</script>'],
  ['missing ingredients', ld({ ...base, recipeIngredient: [] })], ['missing instructions', ld({ ...base, recipeInstructions: [] })],
  ['empty title', ld({ ...base, name: '' })], ['not recipe', ld({ '@type': 'Article' })],
] as const) {
  test(`actionable error for ${name}`, () => assert.throws(() => parseRecipe(html, url), (e) => e instanceof RecipesError && e.code === 'RECIPE_DATA'));
}
test('real reduced search markup returns attributed candidates', async () => {
  const results = parseSearch(await readFile('test/fixtures/search-chicken.html', 'utf8'), 'https://www.recipetineats.com/?s=chicken');
  assert.equal(results.length, 2);
  assert.deepEqual(results[0], { title: 'Oven Baked Chicken Schnitzel', sourceUrl: 'https://www.recipetineats.com/oven-baked-chicken-schnitzel/', provider: 'recipetin-eats' });
});
test('search filters unsupported/unsafe links, deduplicates, decodes entities, caps results, ignores navigation', () => {
  const link = (href: string, title = 'Salt &amp; Pepper') => `<article><a class="entry-title-link" href="${href}">${title}</a></article>`;
  const html = `<a class="entry-title-link" href="/nav/">Navigation</a>` + link('/rice/#recipe') + link('/rice/') +
    link('https://evil.example/recipe/') + link('http://www.recipetineats.com/recipe/') + link('https://me:pw@www.recipetineats.com/') +
    link('/empty/', '') + Array.from({ length: 30 }, (_, i) => link(`/recipe-${i}/`)).join('');
  const results = parseSearch(html, url);
  assert.equal(results.length, 20);
  assert.equal(results[0]?.title, 'Salt & Pepper');
  assert.equal(results[0]?.sourceUrl, 'https://www.recipetineats.com/rice/');
  assert.deepEqual(parseSearch('<html>No results</html>', url), []);
});
test('provider encodes query and uses final fetch URL for attribution', async () => {
  let requested = '';
  const fetchPage = async (input: string) => { requested = input; return { html: ld(base), url }; };
  await recipeTinEats.search('salt & pepper', fetchPage);
  assert.equal(new URL(requested).searchParams.get('s'), 'salt & pepper');
  assert.equal((await recipeTinEats.get(url, fetchPage)).sourceUrl, url);
});
