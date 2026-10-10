import { readFile, mkdir, writeFile, rename, unlink } from 'node:fs/promises';
import { dirname, isAbsolute, join, resolve } from 'node:path';
import { homedir } from 'node:os';
import { randomUUID } from 'node:crypto';
import { RecipesError } from './errors.js';

export interface Config { version: 1; providers: Record<string, boolean> }
export function configPath(explicit?: string, env: NodeJS.ProcessEnv = process.env): string {
  const override = explicit ?? env.RECIPES_CONFIG;
  if (override !== undefined) {
    if (!override.trim()) throw new RecipesError('CONFIG', 'Config path is empty. Set --config or RECIPES_CONFIG to a file path.');
    return resolve(override);
  }
  if (env.XDG_CONFIG_HOME !== undefined && (!env.XDG_CONFIG_HOME || !isAbsolute(env.XDG_CONFIG_HOME))) {
    throw new RecipesError('CONFIG', 'XDG_CONFIG_HOME must be an absolute directory path.');
  }
  return join(env.XDG_CONFIG_HOME ?? join(homedir(), '.config'), 'recipes-cli', 'config.json');
}
const object = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

export class ConfigStore {
  constructor(public readonly path: string, private readonly providerIds: readonly string[]) {}
  async load(): Promise<Config> {
    let raw: string;
    try { raw = await readFile(this.path, 'utf8'); }
    catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') return { version: 1, providers: {} };
      throw new RecipesError('CONFIG', `Cannot read config at ${this.path}. Check the path and file access.`);
    }
    let data: unknown;
    try { data = JSON.parse(raw); }
    catch { throw new RecipesError('CONFIG', `Invalid JSON at ${this.path}. Fix it or move it aside to restore defaults.`); }
    if (!object(data) || data.version !== 1 || !object(data.providers) || Object.keys(data).some((k) => !['version', 'providers'].includes(k))) {
      throw new RecipesError('CONFIG', `Invalid config at ${this.path}. Expected {"version":1,"providers":{"provider-id":true|false}}.`);
    }
    for (const [id, enabled] of Object.entries(data.providers)) {
      if (!this.providerIds.includes(id)) throw new RecipesError('CONFIG', `Unknown provider "${id}" in ${this.path}. Available: ${this.providerIds.join(', ')}.`);
      if (typeof enabled !== 'boolean') throw new RecipesError('CONFIG', `Provider "${id}" must be true or false in ${this.path}.`);
    }
    return data as unknown as Config;
  }
  async setEnabled(id: string, enabled: boolean): Promise<void> {
    if (!this.providerIds.includes(id)) throw new RecipesError('PROVIDER', `Unknown provider "${id}". Available: ${this.providerIds.join(', ')}.`);
    const config = await this.load();
    config.providers[id] = enabled;
    const tmp = `${this.path}.${randomUUID()}.tmp`;
    try {
      await mkdir(dirname(this.path), { recursive: true });
      await writeFile(tmp, `${JSON.stringify(config, null, 2)}\n`, { flag: 'wx' });
      await rename(tmp, this.path);
    } catch {
      throw new RecipesError('CONFIG', `Cannot save config at ${this.path}. Check directory/file access.`);
    } finally { await unlink(tmp).catch(() => {}); }
  }
}
