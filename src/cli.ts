import { parseArgs } from 'node:util';
import { ConfigStore, configPath } from './config.js';
import { createFetcher, type FetchPage } from './http.js';
import { RecipesError } from './errors.js';
import { registry as defaultRegistry } from './providers/index.js';
import type { ProviderRegistry } from './providers/registry.js';
import type { Recipe } from './model.js';

const help = `Recipes CLI — attributed recipes from configurable providers

Usage:
  recipes search QUERY [--provider ID] [--json]
  recipes get HTTPS_URL [--provider ID] [--json]
  recipes providers status [--json]
  recipes providers enable ID
  recipes providers disable ID

Options:
  --config FILE   Config file (overrides RECIPES_CONFIG and XDG_CONFIG_HOME)
  --json          Machine-readable JSON output
  --help, -h      Show help
  --version       Show version

Search reads one page (up to 20 candidate posts per provider), not all recipes.
`;
interface Dependencies {
  registry?: ProviderRegistry;
  fetchPage?: FetchPage;
  env?: NodeJS.ProcessEnv;
  stdout?: (text: string) => void;
  stderr?: (text: string) => void;
}
function recipeText(recipe: Recipe): string {
  return [recipe.title, `Source: ${recipe.sourceUrl} (${recipe.provider})`,
    ...(recipe.metadata?.author ? [`Author: ${recipe.metadata.author}`] : []),
    '', 'Ingredients:', ...recipe.ingredients.map((s) => `- ${s}`),
    '', 'Instructions:', ...recipe.instructions.map((s, i) => `${i + 1}. ${s}`),
    '', 'See source for notes, substitutions and safety/allergen guidance.'].join('\n');
}
export async function run(args: string[], deps: Dependencies = {}): Promise<number> {
  const stdout = deps.stdout ?? ((s) => process.stdout.write(s));
  const stderr = deps.stderr ?? ((s) => process.stderr.write(s));
  const registry = deps.registry ?? defaultRegistry;
  const fetchPage = deps.fetchPage ?? createFetcher();
  try {
    let parsed: ReturnType<typeof parseArgs>;
    try {
      parsed = parseArgs({ args, allowPositionals: true, options: {
        config: { type: 'string' }, provider: { type: 'string' }, json: { type: 'boolean' },
        help: { type: 'boolean', short: 'h' }, version: { type: 'boolean' },
      } });
    } catch (error) { throw new RecipesError('USAGE', `${(error as Error).message}. Run recipes --help.`); }
    const { values, positionals } = parsed;
    if (values.help || !args.length) { stdout(help); return 0; }
    if (values.version) { stdout('0.1.0\n'); return 0; }
    const [command, ...rest] = positionals;
    const selected = values.provider as string | undefined;
    if (selected !== undefined && !selected.trim()) throw new RecipesError('USAGE', '--provider requires a non-empty provider ID.');
    if (command !== 'search' && command !== 'get' && command !== 'providers') throw new RecipesError('USAGE', 'Unknown command. Run recipes --help.');
    if ((command === 'search' && !rest.length) || (command === 'get' && rest.length !== 1)) throw new RecipesError('USAGE', `recipes ${command} requires ${command === 'search' ? 'a query' : 'one HTTPS URL'}. Run recipes --help.`);
    if (command === 'providers' && (selected !== undefined || !(
      (rest[0] === 'status' && rest.length === 1) ||
      (['enable', 'disable'].includes(rest[0] ?? '') && rest.length === 2 && !values.json)
    ))) throw new RecipesError('USAGE', 'Use recipes providers status [--json], enable ID or disable ID.');
    const store = new ConfigStore(configPath(values.config as string | undefined, deps.env), registry.ids);
    const config = await store.load();
    if (command === 'providers') {
      if (rest[0] === 'status') {
        const data = { configPath: store.path, providers: registry.status(config) };
        stdout(values.json ? `${JSON.stringify(data, null, 2)}\n` : `Config: ${store.path}\n${data.providers.map((p) => `${p.id}: ${p.enabled ? 'enabled' : 'disabled'} (${p.name})`).join('\n')}\n`);
      } else {
        await store.setEnabled(rest[1]!, rest[0] === 'enable');
        stdout(`${rest[1]} ${rest[0] === 'enable' ? 'enabled' : 'disabled'}. Config: ${store.path}\n`);
      }
    } else if (command === 'search') {
      const results = await registry.search(rest.join(' '), config, fetchPage, selected);
      stdout(values.json ? `${JSON.stringify(results, null, 2)}\n` : `${results.map((r) => `${r.title}\n  ${r.sourceUrl} (${r.provider})`).join('\n')}\n`);
    } else {
      const recipe = await registry.get(rest[0]!, config, fetchPage, selected);
      stdout(values.json ? `${JSON.stringify(recipe, null, 2)}\n` : `${recipeText(recipe)}\n`);
    }
    return 0;
  } catch (error) {
    if (error instanceof RecipesError) {
      stderr(`Error [${error.code}]: ${error.message}\n`);
      return error.code === 'USAGE' ? 2 : 1;
    }
    stderr('Error [INTERNAL]: Unexpected failure. Report this command at https://github.com/denwilliams/recipes-cli/issues.\n');
    return 1;
  }
}
