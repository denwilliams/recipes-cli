import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, readdir, mkdir, rm } from 'node:fs/promises';
import { tmpdir, homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { ConfigStore, configPath } from '../src/config.js';
import { RecipesError } from '../src/errors.js';

test('config path precedence, XDG and default', () => {
  assert.equal(configPath('explicit.json', { RECIPES_CONFIG: '/env.json' }), resolve('explicit.json'));
  assert.equal(configPath(undefined, { RECIPES_CONFIG: '/env.json' }), '/env.json');
  assert.equal(configPath(undefined, { XDG_CONFIG_HOME: '/xdg' }), '/xdg/recipes-cli/config.json');
  assert.equal(configPath(undefined, {}), join(homedir(), '.config', 'recipes-cli', 'config.json'));
  for (const env of [{ RECIPES_CONFIG: '' }, { XDG_CONFIG_HOME: 'relative' }, { XDG_CONFIG_HOME: '' }]) assert.throws(() => configPath(undefined, env), /Config path is empty|absolute/);
});
test('missing file is read-only defaults; enable/disable persists overrides across new stores', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'recipes-config-')); t.after(() => rm(dir, { recursive: true, force: true }));
  const path = join(dir, 'nested', 'config.json');
  const store = new ConfigStore(path, ['a', 'b']);
  assert.deepEqual(await store.load(), { version: 1, providers: {} });
  assert.deepEqual(await readdir(dir), []);
  await store.setEnabled('a', false);
  await new ConfigStore(path, ['a', 'b']).setEnabled('b', true);
  assert.deepEqual(await store.load(), { version: 1, providers: { a: false, b: true } });
  await store.setEnabled('a', true);
  assert.deepEqual(JSON.parse(await readFile(path, 'utf8')), { version: 1, providers: { a: true, b: true } });
  assert.deepEqual(await readdir(join(dir, 'nested')), ['config.json']);
  await assert.rejects(store.setEnabled('unknown', false), (e) => e instanceof RecipesError && e.code === 'PROVIDER');
});
test('malformed/unknown/versioned config fails without overwriting original', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'recipes-config-')); t.after(() => rm(dir, { recursive: true, force: true }));
  const path = join(dir, 'config.json');
  const store = new ConfigStore(path, ['a']);
  for (const raw of ['{bad', 'null', '[]', '{}', '{"version":2,"providers":{}}', '{"version":1,"providers":[]}', '{"version":1,"providers":{"a":"false"}}', '{"version":1,"providers":{"unknown":true}}', '{"version":1,"providers":{},"typo":1}']) {
    await writeFile(path, raw);
    await assert.rejects(store.load(), (e) => e instanceof RecipesError && e.code === 'CONFIG');
    await assert.rejects(store.setEnabled('a', false), (e) => e instanceof RecipesError && e.code === 'CONFIG');
    assert.equal(await readFile(path, 'utf8'), raw);
  }
});
test('config read and write failures are actionable and clean temporary files', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'recipes-config-')); t.after(() => rm(dir, { recursive: true, force: true }));
  const store = new ConfigStore(dir, ['a']);
  await assert.rejects(store.load(), /Cannot read config/);
  const blocker = join(dir, 'blocker'); await writeFile(blocker, 'file');
  await assert.rejects(new ConfigStore(join(blocker, 'config.json'), ['a']).setEnabled('a', false), /Cannot read config|Cannot save config/);
  // Renaming a completed temp file onto a non-empty directory fails safely.
  const destination = join(dir, 'destination'); await mkdir(destination); await writeFile(join(destination, 'keep'), 'keep');
  const failing = new ConfigStore(destination, ['a']);
  failing.load = async () => ({ version: 1, providers: {} });
  await assert.rejects(failing.setEnabled('a', false), /Cannot save config/);
  assert.ok(!(await readdir(dir)).some((name) => name.endsWith('.tmp')));
  assert.equal(await readFile(join(destination, 'keep'), 'utf8'), 'keep');
});
