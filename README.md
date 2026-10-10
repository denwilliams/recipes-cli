# Recipes CLI

An installable CLI for discovering recipe candidates and retrieving attributed recipes.
The first version supports **RecipeTin Eats only**. It is not affiliated with that site.

## Stack and installation

Requires **Node.js 22 or newer** and npm. TypeScript makes the provider/model boundary explicit;
Node supplies argument parsing, HTTPS and the test runner. Cheerio handles HTML, and ipaddr.js
validates public IP ranges. No browser automation, API keys or cloud service.

Nothing has been published to npm. Install from a checkout:

```sh
git clone https://github.com/denwilliams/recipes-cli.git
cd recipes-cli
npm ci
npm run build
# Run without global installation:
node dist/src/bin.js --help
# Or create a local package and install its `recipes` executable:
npm pack
npm install --global ./denwilliams-recipes-cli-0.1.0.tgz
recipes --help
```

`npm pack` runs the build. The package includes compiled application files, README and LICENSE,
not fixtures/tests. A custom `--prefix /path/to/prefix` can be used with global install to avoid
system-directory access; ensure that prefix's `bin` is on PATH. No release publication is required.

## Commands

```sh
recipes search "chicken"
recipes search "salt & pepper" --provider recipetin-eats --json
recipes get https://www.recipetineats.com/butter-chicken/
recipes get https://www.recipetineats.com/butter-chicken/ --json
recipes providers status
recipes providers status --json
recipes providers disable recipetin-eats
recipes providers enable recipetin-eats
recipes --config ./my-recipes.json providers status
```

`search` discovers candidate **posts** with title, sourceUrl and provider; it does not fetch or
verify each recipe. `get` retrieves one page and requires a complete structured Recipe.
Normal output is plain text. `--json` emits a result array for search, a recipe object for get,
or a status object for provider status. Errors go to stderr, never mixed into JSON.
Exit codes: `0` success/help, `1` provider/config/network/data/empty-result error, `2` usage error.

The normalized recipe contains `title`, `sourceUrl`, `provider`, `ingredients: string[]`, and
`instructions: string[]`. Optional `metadata` includes author, description, yield (array), and
prep/cook/total time (source ISO 8601 duration strings). Instructions are ordered plain text;
HowToSections are flattened. No unit conversion, serving scaling or nutritional interpretation.
Source URLs reflect the final validated redirect URL, not a site-supplied untrusted canonical URL.

## Persistent provider configuration

RecipeTin Eats (`recipetin-eats`) is enabled by default. A missing config uses defaults without
creating a file. Enable/disable commands persist overrides with an atomic temporary-file rename.
A disabled provider is never invoked for search **or URL retrieval**, so it makes no network calls.

Path precedence:
1. `--config FILE` (relative paths resolve from the current working directory)
2. `RECIPES_CONFIG` environment variable
3. `$XDG_CONFIG_HOME/recipes-cli/config.json` if XDG_CONFIG_HOME is set to an absolute directory
4. `~/.config/recipes-cli/config.json`

Example:

```json
{
  "version": 1,
  "providers": {
    "recipetin-eats": false
  }
}
```

Omitted providers retain their registered default. Values must be booleans, IDs must be registered,
and only `version` and `providers` are accepted at the top level. Malformed/unsupported config
fails clearly and is not overwritten: fix it or move it aside to restore defaults. Status shows the
resolved path. Concurrent config edits are not locked; avoid simultaneous enable/disable writes.

## Actual site support and limits

Inspected on **2026-10-10**:
- [Public search](https://www.recipetineats.com/?s=chicken) returns server-rendered
  `article .entry-title-link` elements: 20 candidates on the observed page, with pagination.
  This CLI reads **only the first search page**, returning at most 20 candidates, preserving site
  order. No pagination, category browsing, exhaustive recipe index or promises about ranking.
- [Butter Chicken](https://www.recipetineats.com/butter-chicken/) embeds schema.org Recipe
  in a Yoast JSON-LD `@graph`, with ingredients and HowToStep instructions. Retrieval uses that
  structured data (also standalone Recipe nodes/arrays), not arbitrary article prose.
- [robots.txt](https://www.recipetineats.com/robots.txt) allowed the general user-agent at inspection.
  The CLI identifies itself as `recipes-cli/0.1.0`; it does not fetch robots.txt each time or claim
  perpetual permission. Use responsibly, check current site terms/restrictions, and stop if denied.

Search may include non-recipe posts. Empty results may mean no matches, a changed site layout or
an access challenge. Missing/malformed/incomplete structured data gives an actionable error.
No JavaScript execution, challenge/CAPTCHA solving, impersonation, login, restriction bypass,
retries or bulk crawling. Site content changes can break discovery/parsing. Proxy environment
variables and NAT64-only/IPv6-special-service deployments are not supported. Compressed responses
are rejected if a server ignores the requested identity encoding. HTML is decoded as UTF-8, matching the inspected site.

Network safeguards: HTTPS only on the provider's exact registered hosts (`www.recipetineats.com`
and `recipetineats.com`); no credentials/non-default ports. Every redirect is validated before its
next request, with at most 3 redirects. All DNS answers must be public, and a validated address is
pinned for the socket while retaining hostname-based TLS verification. IPv4-mapped IPv6 uses the
IPv4 policy. IPv6 must match an explicitly **ALLOCATED regional-registry prefix** in the static
[IANA Global Unicast snapshot](https://www.iana.org/assignments/ipv6-unicast-address-assignments/).
The broad `2000::/3` envelope is NOT an allowlist: IANA-reserved/unlisted gaps inside it are refused,
as are reserved/future allocations outside it. Special-purpose exclusions remain (including
protocol assignments, documentation, transition, former 6bone and special-service blocks).
Local-use NAT64, site-local, dummy/SRv6 and mapped-private addresses therefore cannot reach the
transport. Neither a library's residual `unicast` label nor presence in `2000::/3` proves public use.
Even some globally reachable translation/special-service addresses are deliberately unsupported.
The static policy requires maintenance as allocations change and does not promise routability.
Sources: the global-unicast registry (updated 2025-10-10),
[IANA IPv6 address space](https://www.iana.org/assignments/ipv6-address-space/) and
[special-purpose registry](https://www.iana.org/assignments/iana-ipv6-special-registry/), read on
2026-10-10. A mixed public/refused DNS answer is rejected entirely before transport, including
after redirects; there is no address filtering/fallback. One 10-second deadline covers DNS,
redirects, headers and body; responses are limited to 2 MiB. DNS lookups cannot be
cancelled at OS level, but late answers are ignored and cannot start a request after timeout.
No fallback to private addresses or another host. No guarantee of connectivity: an unreachable
first public address produces an error rather than retrying all DNS answers.

Recipes and site content belong to their original authors. Always preserve attribution and consult
the source for notes, substitutions, ingredient grouping and safety/allergen guidance. JSON-LD
may omit recipe-card notes/groups, images, videos and marinating-time details. This MVP does not
reproduce those, even when ingredient/instruction text refers to a numbered note. The repository
MIT license applies to software, not third-party recipes.

Non-goals: other production providers, dynamically installed plugins, cloud/login services, bulk
crawling, meal planning, deployment or publishing releases.

## Offline checks and optional live smoke

```sh
npm ci
npm run typecheck
npm test
npm run build
npm pack --dry-run
```

Offline tests use reduced attributed HTML fixtures and injected DNS/transport/provider mocks.
They cover JSON-LD variants and errors, search filtering, provider dispatch/defaults, enable/disable
persistence and zero-network disabled paths, CLI text/JSON/exit codes, package entrypoint, HTTP/DNS/
TLS/stream failures, public-address checks, redirects, response limits and timeouts. Allocation-policy tests compare
against a separately captured IANA table, including all allocated/reserved prefix endpoints and
both ends of every unlisted gap, plus zero-transport initial/mixed/redirect DNS cases. The native
HTTPS adapter is also tested without sockets, including Node's single/all DNS callback forms.
Fixture provenance is in `test/fixtures/README.md`. Tests create isolated temporary config files.

Optional, network-dependent and **not** part of tests/CI:

```sh
npm run smoke:live
```

This fetches one chicken search page and one Butter Chicken page, plus any validated redirects,
without retries or pagination. It checks normalized data, not search ranking, and does not update
fixtures or touch user config. A site denial or network failure is reported, never bypassed.

## Adding a second built-in provider

1. Implement `Provider` from `src/providers/provider.ts` in a new module. Choose a unique stable
   ID, display name, exact public HTTPS hosts and default enabled state. Implement `matches`,
   `search` and `get`; keep provider-specific URLs and parsing inside that module.
2. Use the injected `FetchPage` with your host allowlist, return normalized Recipe/SearchResult
   models with source attribution, and throw `RecipesError` for actionable failures. Discovery
   should remain bounded. Inspect the actual provider's public data and restrictions first.
3. Import and add it to the array in `src/providers/index.ts`. **No CLI command logic changes**:
   registry dispatch, status, configuration and enable/disable work for every registered provider.
4. Add attributed offline fixtures and deterministic tests. Test empty/missing data and disabled
   paths. A synthetic second provider test demonstrates unchanged CLI dispatch; no second
   production provider ships in this MVP.

The registry aggregates enabled-provider searches, failing the command if any selected provider
fails (no silent partial results). Retrieval selects a matching registered provider. Providers
should have disjoint host ownership; dynamically installed plugins are deliberately out of scope.

### Maintaining the address policy

`src/address-policy.ts` contains the reviewed static IPv6 RIR allocation allowlist and additional
special-purpose exclusions. To update it, inspect the linked current IANA registries; never
auto-enable all of `2000::/3`, unlisted space, transition prefixes or IANA special allocations.
Refresh `test/fixtures/iana-global-unicast.json` with the actual registry source/date and table,
review each policy change against the special-purpose registry, then run all offline checks.
The runtime never fetches these registries. New public allocations remain unsupported until
a reviewed software update; failing closed is preferred to silently widening network access.
