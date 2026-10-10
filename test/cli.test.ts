import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { run } from '../src/cli.js';
import { ProviderRegistry } from '../src/providers/registry.js';
import type { Provider } from '../src/providers/provider.js';
import type { FetchPage } from '../src/http.js';
import { RecipesError } from '../src/errors.js';

async function setup(t: { after(fn: () => Promise<unknown>): void }) {
  const dir = await mkdtemp(join(tmpdir(), 'recipes-cli-')); t.after(() => rm(dir, { recursive: true, force: true }));
  const path = join(dir, 'config.json');
  let calls = 0;
  const fetchPage: FetchPage = async (url) => {
    calls++;
    const search = new URL(url).searchParams.has('s');
    return { html: await readFile(`test/fixtures/${search ? 'search-chicken' : 'butter-chicken'}.html`, 'utf8'), url };
  };
  async function invoke(args: string[], overrides: Parameters<typeof run>[1] = {}) {
    let out = '', err = '';
    const exit = await run(args, { env: { RECIPES_CONFIG: path }, fetchPage, stdout: (s) => { out += s; }, stderr: (s) => { err += s; }, ...overrides });
    return { exit, out, err };
  }
  return { path, invoke, calls: () => calls };
}
test('help and version work without config/network; invalid syntax has exit 2', async (t) => {
  const ctx = await setup(t);
  await writeFile(ctx.path, 'invalid');
  for (const args of [[], ['--help'], ['-h']]) {
    const result = await ctx.invoke(args); assert.equal(result.exit, 0); assert.match(result.out, /Usage:/);
  }
  assert.equal((await ctx.invoke(['--version'])).out, '0.1.0\n');
  for (const args of [['unknown'], ['search'], ['get'], ['get', 'one', 'two'], ['--not-an-option'], ['providers'], ['providers', 'status', 'extra'], ['providers', 'enable'], ['providers', 'disable', 'recipetin-eats', '--json'], ['providers', 'status', '--provider', 'recipetin-eats']]) {
    const result = await ctx.invoke(args); assert.equal(result.exit, 2, args.join(' ')); assert.equal(result.out, ''); assert.match(result.err, /USAGE/);
  }
  assert.equal(ctx.calls(), 0);
});
test('search/get CLI text and JSON include source attribution', async (t) => {
  const ctx = await setup(t);
  const search = await ctx.invoke(['search', 'chicken', '--json']);
  assert.equal(search.exit, 0); assert.equal(JSON.parse(search.out)[0].provider, 'recipetin-eats'); assert.equal(search.err, '');
  const textSearch = await ctx.invoke(['search', 'chicken', 'thigh', '--provider', 'recipetin-eats']);
  assert.match(textSearch.out, /Oven Baked Chicken Schnitzel\n  https:/);
  const url = 'https://www.recipetineats.com/butter-chicken/';
  const result = await ctx.invoke(['get', url, '--json']);
  assert.equal(result.exit, 0); assert.equal(JSON.parse(result.out).sourceUrl, url); assert.equal(JSON.parse(result.out).instructions.length, 7);
  const textResult = await ctx.invoke(['get', url]);
  assert.match(textResult.out, /Source: https:\/\/www.recipetineats.com\/butter-chicken\//);
  assert.match(textResult.out, /Ingredients:\n- 1\/2 cup/); assert.match(textResult.out, /Instructions:\n1\./);
  assert.equal(ctx.calls(), 4);
});
test('provider status defaults then persisted disable blocks discovery/get before network; enable restores it', async (t) => {
  const ctx = await setup(t);
  const status = await ctx.invoke(['providers', 'status', '--json']);
  assert.equal(JSON.parse(status.out).providers[0].enabled, true);
  assert.equal(JSON.parse(status.out).configPath, ctx.path);
  assert.equal((await ctx.invoke(['providers', 'disable', 'recipetin-eats'])).exit, 0);
  assert.match((await ctx.invoke(['providers', 'status'])).out, /recipetin-eats: disabled/);
  for (const args of [['search', 'chicken'], ['search', 'chicken', '--provider', 'recipetin-eats'], ['get', 'https://www.recipetineats.com/butter-chicken/']]) {
    const result = await ctx.invoke(args); assert.equal(result.exit, 1); assert.equal(result.out, ''); assert.match(result.err, /DISABLED/);
  }
  assert.equal(ctx.calls(), 0);
  assert.equal((await ctx.invoke(['providers', 'enable', 'recipetin-eats'])).exit, 0);
  assert.equal((await ctx.invoke(['search', 'chicken'])).exit, 0);
  assert.equal(ctx.calls(), 1);
});
test('CLI errors for unknown provider, unsupported URL, bad config, empty and network failures', async (t) => {
  const ctx = await setup(t);
  for (const args of [['search', 'chicken', '--provider', 'missing'], ['providers', 'enable', 'missing'], ['providers', 'disable', 'missing'], ['get', 'https://evil.example/']]) {
    const result = await ctx.invoke(args); assert.equal(result.exit, 1); assert.equal(result.out, ''); assert.match(result.err, /PROVIDER|URL/);
  }
  for (const args of [['search', '  '], ['search', 'chicken', '--provider', '']]) assert.equal((await ctx.invoke(args)).exit, 2);
  assert.equal(ctx.calls(), 0);
  const empty = await ctx.invoke(['search', 'x'], { fetchPage: async (url) => ({ html: 'no results', url }) });
  assert.match(empty.err, /EMPTY/); assert.equal(empty.exit, 1);
  const missing = await ctx.invoke(['get', 'https://www.recipetineats.com/no-recipe/'], { fetchPage: async (url) => ({ html: 'no recipe', url }) });
  assert.match(missing.err, /RECIPE_DATA/); assert.equal(missing.exit, 1);
  for (const code of ['HTTP', 'TIMEOUT', 'SIZE', 'NETWORK']) {
    const failure = await ctx.invoke(['search', 'x'], { fetchPage: async () => { throw new RecipesError(code, 'Try later'); } });
    assert.equal(failure.exit, 1); assert.equal(failure.out, ''); assert.match(failure.err, new RegExp(code));
  }
  await writeFile(ctx.path, '{invalid');
  const invalid = await ctx.invoke(['search', 'chicken']); assert.equal(invalid.exit, 1); assert.match(invalid.err, /CONFIG.*Invalid JSON/);
  assert.equal(ctx.calls(), 0);
});
test('explicit --config overrides environment config', async (t) => {
  const ctx = await setup(t);
  await writeFile(ctx.path, 'bad JSON');
  const other = `${ctx.path}.other`;
  assert.equal((await ctx.invoke(['providers', 'disable', 'recipetin-eats', '--config', other])).exit, 0);
  assert.equal(JSON.parse(await readFile(other, 'utf8')).providers['recipetin-eats'], false);
  assert.equal(await readFile(ctx.path, 'utf8'), 'bad JSON');
});
test('second synthetic provider works through unchanged CLI dispatch/config', async (t) => {
  const ctx = await setup(t);
  const second: Provider = {
    id: 'second', name: 'Second fixture provider', hosts: ['second.example'], enabledByDefault: false,
    matches: (u) => u.hostname === 'second.example',
    async search() { return [{ title: 'Salt', sourceUrl: 'https://second.example/salt/', provider: 'second' }]; },
    async get(url) { return { title: 'Salt', sourceUrl: url, provider: 'second', ingredients: ['Salt'], instructions: ['Use'] }; },
  };
  const registry = new ProviderRegistry([second]);
  assert.equal((await ctx.invoke(['providers', 'enable', 'second'], { registry })).exit, 0);
  const found = await ctx.invoke(['search', 'salt', '--provider', 'second', '--json'], { registry });
  assert.equal(found.exit, 0); assert.equal(JSON.parse(found.out)[0].provider, 'second');
  assert.equal((await ctx.invoke(['get', 'https://second.example/salt/'], { registry })).exit, 0);
  assert.equal(ctx.calls(), 0);
});
test('real entrypoint prints help/status and gives nonzero exit on disabled retrieval', async (t) => {
  const ctx = await setup(t);
  const execute = (args: string[]) => spawnSync(process.execPath, ['dist/src/bin.js', ...args], { encoding: 'utf8', env: { ...process.env, RECIPES_CONFIG: ctx.path } });
  assert.equal(execute(['--help']).status, 0);
  assert.equal(execute(['providers', 'disable', 'recipetin-eats']).status, 0);
  const status = execute(['providers', 'status', '--json']); assert.equal(status.status, 0); assert.equal(JSON.parse(status.stdout).providers[0].enabled, false);
  const result = execute(['get', 'https://www.recipetineats.com/butter-chicken/']);
  assert.equal(result.status, 1); assert.equal(result.stdout, ''); assert.match(result.stderr, /DISABLED/);
});
test('executable still runs through an npm-style bin symlink', async (t) => {
  const ctx = await setup(t);
  const { symlink } = await import('node:fs/promises');
  const { resolve } = await import('node:path');
  const link = `${ctx.path}.recipes`;
  await symlink(resolve('dist/src/bin.js'), link);
  const result = spawnSync(process.execPath, [link, '--version'], { encoding: 'utf8', env: { ...process.env, RECIPES_CONFIG: ctx.path } });
  assert.equal(result.status, 0); assert.equal(result.stdout, '0.1.0\n'); assert.equal(result.stderr, '');
});
