import type { Provider } from './provider.js';
import type { Config } from '../config.js';
import type { FetchPage } from '../http.js';
import { validateUrl } from '../http.js';
import { RecipesError } from '../errors.js';

export class ProviderRegistry {
  private readonly byId: Map<string, Provider>;
  constructor(private readonly providers: readonly Provider[]) {
    this.byId = new Map(providers.map((p) => [p.id, p]));
    if (this.byId.size !== providers.length) throw new Error('Duplicate provider ids');
  }
  get ids(): string[] { return [...this.byId.keys()]; }
  private require(id: string): Provider {
    const provider = this.byId.get(id);
    if (!provider) throw new RecipesError('PROVIDER', `Unknown provider "${id}". Available: ${this.ids.join(', ')}.`);
    return provider;
  }
  private enabled(provider: Provider, config: Config): boolean {
    return config.providers[provider.id] ?? provider.enabledByDefault;
  }
  status(config: Config) {
    return this.providers.map((p) => ({ id: p.id, name: p.name, enabled: this.enabled(p, config) }));
  }
  async search(query: string, config: Config, fetchPage: FetchPage, selected?: string) {
    if (!query.trim()) throw new RecipesError('USAGE', 'Search query cannot be empty. Example: recipes search "chicken".');
    if (query.length > 200) throw new RecipesError('USAGE', 'Search query is too long (maximum 200 characters).');
    const candidates = selected ? [this.require(selected)] : this.providers;
    const enabled = candidates.filter((p) => this.enabled(p, config));
    if (!enabled.length) throw new RecipesError('DISABLED', 'No selected providers are enabled. Run recipes providers enable PROVIDER-ID.');
    const results = (await Promise.all(enabled.map((p) => p.search(query.trim(), fetchPage)))).flat();
    if (!results.length) throw new RecipesError('EMPTY', 'No recipe candidates found. Try another query; site layout changes or challenge pages may also cause empty results.');
    return results;
  }
  async get(input: string, config: Config, fetchPage: FetchPage, selected?: string) {
    const candidates = selected ? [this.require(selected)] : this.providers;
    const url = validateUrl(input, candidates.flatMap((p) => [...p.hosts]));
    const provider = candidates.find((p) => p.matches(url));
    if (!provider) throw new RecipesError('URL', 'No provider supports this URL. Run recipes providers status for available providers.');
    if (!this.enabled(provider, config)) throw new RecipesError('DISABLED', `Provider "${provider.id}" is disabled. Run recipes providers enable ${provider.id}.`);
    return provider.get(url.href, fetchPage);
  }
}
