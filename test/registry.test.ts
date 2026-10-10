import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ProviderRegistry } from '../src/providers/registry.js';
import type { Provider } from '../src/providers/provider.js';
import { RecipesError } from '../src/errors.js';
const defaultConfig = { version: 1, providers: {} } as const;
const noFetch = async () => { throw new Error('Network must not be invoked'); };
function provider(id: string, calls: string[]): Provider {
  return {
    id, name: id, hosts: [`${id}.example`], enabledByDefault: true,
    matches: (url) => url.hostname === `${id}.example`,
    async search() { calls.push(`${id}:search`); return [{ title: id, sourceUrl: `https://${id}.example/`, provider: id }]; },
    async get(url) { calls.push(`${id}:get`); return { title: id, sourceUrl: url, provider: id, ingredients: ['salt'], instructions: ['Mix'] }; },
  };
}
const code = (expected: string) => (error: unknown) => error instanceof RecipesError && error.code === expected;

test('registry discovers only enabled providers, selected provider dispatch, get matches correct host', async () => {
  const calls: string[] = [];
  const registry = new ProviderRegistry([provider('a', calls), provider('b', calls)]);
  const config = { version: 1, providers: { a: false } } as const;
  assert.deepEqual((await registry.search('salt', config, noFetch)).map((r) => r.provider), ['b']);
  await registry.search('salt', defaultConfig, noFetch, 'a');
  await registry.get('https://b.example/recipe/', defaultConfig, noFetch);
  assert.deepEqual(calls, ['b:search', 'a:search', 'b:get']);
  assert.deepEqual(registry.status(config).map((p) => p.enabled), [false, true]);
});
test('disabled discovery and retrieval do not invoke providers or fetch', async () => {
  const calls: string[] = [];
  const registry = new ProviderRegistry([provider('a', calls)]);
  const config = { version: 1, providers: { a: false } } as const;
  await assert.rejects(registry.search('salt', config, noFetch), code('DISABLED'));
  await assert.rejects(registry.search('salt', config, noFetch, 'a'), code('DISABLED'));
  await assert.rejects(registry.get('https://a.example/', config, noFetch), code('DISABLED'));
  await assert.rejects(registry.get('https://a.example/', config, noFetch, 'a'), code('DISABLED'));
  assert.deepEqual(calls, []);
});
test('unknown provider, unsafe URL, host mismatch, empty query and empty results fail without network', async () => {
  const calls: string[] = [];
  const a = provider('a', calls);
  a.search = async () => [];
  const registry = new ProviderRegistry([a]);
  await assert.rejects(registry.search('x', defaultConfig, noFetch, 'missing'), code('PROVIDER'));
  await assert.rejects(registry.get('https://a.example/', defaultConfig, noFetch, 'missing'), code('PROVIDER'));
  for (const url of ['not a url', 'http://a.example/', 'https://other.example/', 'https://u:p@a.example/', 'https://a.example:444/']) {
    await assert.rejects(registry.get(url, defaultConfig, noFetch), code('URL'));
  }
  await assert.rejects(registry.search('  ', defaultConfig, noFetch), code('USAGE'));
  await assert.rejects(registry.search('x'.repeat(201), defaultConfig, noFetch), code('USAGE'));
  await assert.rejects(registry.search('salt', defaultConfig, noFetch), code('EMPTY'));
  assert.deepEqual(calls, []);
});
test('provider defaults and duplicate ids', () => {
  const a = { ...provider('a', []), enabledByDefault: false };
  const registry = new ProviderRegistry([a]);
  assert.equal(registry.status(defaultConfig)[0]?.enabled, false);
  assert.equal(registry.status({ version: 1, providers: { a: true } })[0]?.enabled, true);
  assert.throws(() => new ProviderRegistry([a, a]), /Duplicate/);
});
