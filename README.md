# Recipes CLI — Go alternative

Standalone native CLI for attributed RecipeTin Eats discovery and retrieval. This is
a separate Go implementation of [issue #1](https://github.com/denwilliams/recipes-cli/issues/1),
merged to `main` in [PR #3](https://github.com/denwilliams/recipes-cli/pull/3); the alternative TypeScript [PR #2](https://github.com/denwilliams/recipes-cli/pull/2)
is left unchanged. RecipeTin Eats is the **only production provider** in v1.
No cloud service, login, API key, browser automation, Node or npm requirement.

## Download and install a release

When a release is available, download from
[GitHub Releases](https://github.com/denwilliams/recipes-cli/releases).
The executable is standalone: no Go, Node, npm or C runtime installation needed.
Choose the archive for your **OS and CPU**, not the OS used to download it:

| OS | Architecture | Archive |
| --- | --- | --- |
| Linux | x86-64 / ARM64 | `recipes_vX.Y.Z_linux_amd64.tar.gz` / `recipes_vX.Y.Z_linux_arm64.tar.gz` |
| macOS | Intel / Apple Silicon | `recipes_vX.Y.Z_darwin_amd64.tar.gz` / `recipes_vX.Y.Z_darwin_arm64.tar.gz` |
| Windows | x86-64 / ARM64 | `recipes_vX.Y.Z_windows_amd64.zip` / `recipes_vX.Y.Z_windows_arm64.zip` |

Download `SHA256SUMS` from the **same release** and verify your archive before
extracting. SHA-256 detects download corruption; it is not a separate signing or
notarization mechanism. Only download from the repository's trusted Releases page.
Example for Linux x86-64 (replace `vX.Y.Z` with your chosen published tag):

```sh
archive=recipes_vX.Y.Z_linux_amd64.tar.gz
grep "  $archive$" SHA256SUMS > selected.sha256
# Linux:
sha256sum -c selected.sha256
# macOS instead: shasum -a 256 -c selected.sha256
mkdir recipes-release
tar -xzf "$archive" -C recipes-release
./recipes-release/recipes --version          # exactly the release tag
./recipes-release/recipes --help
mkdir -p "$HOME/.local/bin"
cp recipes-release/recipes "$HOME/.local/bin/recipes"
# Add $HOME/.local/bin to PATH if desired.
```

Windows PowerShell (replace the archive name/version as needed):

```powershell
$archive = "recipes_vX.Y.Z_windows_amd64.zip"
$line = @(Get-Content SHA256SUMS | Where-Object { $_.EndsWith("  $archive") })
if ($line.Count -ne 1) { throw "Missing or ambiguous checksum" }
$expected = $line[0].Split(" ")[0]
if ((Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) { throw "Checksum mismatch" }
Expand-Archive $archive -DestinationPath recipes-release
.\recipes-release\recipes.exe --version
.\recipes-release\recipes.exe --help
# Keep recipes.exe in a user-owned directory and optionally add that directory to PATH.
```

Archives contain the executable, `LICENSE` and this `README.md`. Linux builds use
`CGO_ENABLED=0` (no glibc dependency); macOS and Windows retain normal OS facilities
such as DNS and system certificate stores. No installer, code signing, macOS
notarization or Windows Authenticode is provided; follow your OS security policy,
rather than disabling protections. Native smoke gates cover all six OS/architecture
combinations, but are not proof of compatibility with every older OS/CPU revision.

## Build from source

Build-time requirements: Go **1.26+**, Git and internet access for the pinned
`golang.org/x/net/html` parser dependency on the first build (see `go.mod`/`go.sum`).
All other functionality uses the Go standard library. Go 1.26 is required by the
current parser dependency; the initial issue comment's proposed 1.25 minimum was
updated after dependency inspection. The executable needs **no Go or Node runtime**.

```sh
go mod download
CGO_ENABLED=0 go build -trimpath -o bin/recipes ./cmd/recipes
./bin/recipes --version
./bin/recipes --help
# Optional user-local installation (no system-wide installation required):
mkdir -p "$HOME/.local/bin"
cp ./bin/recipes "$HOME/.local/bin/recipes"
# Add $HOME/.local/bin to PATH if desired.
```

Build from a reviewed Go commit on `main`, not from the TypeScript branch. Binaries
are not checked into the repository. Unversioned builds retain `0.1.0-go`; release
builds inject the complete tag (e.g. `v1.2.3`) into `--version`. The provider
HTTP user-agent remains `recipes-cli/0.1.0-go` independently of release version. Build for
the target OS/architecture, for example:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/recipes-linux-amd64 ./cmd/recipes
```

## Use

```sh
./bin/recipes search chicken
./bin/recipes search "salt & pepper" --provider recipetin-eats --json
./bin/recipes get https://www.recipetineats.com/butter-chicken/
./bin/recipes get https://www.recipetineats.com/butter-chicken/ --json
./bin/recipes providers status --json
./bin/recipes --config ./local-config.json providers disable recipetin-eats
./bin/recipes --config ./local-config.json search chicken # DISABLED; no network
./bin/recipes --config ./local-config.json providers enable recipetin-eats
```

Options work before or after positional arguments; `--config=FILE` and
`--provider=ID` also work. `--` ends option parsing (e.g. `search -- -chicken`).
Help/version do not read config or use the network. Exit 0 means success, 2 means
usage error, 1 means config/provider/URL/data/network failure. Errors are actionable,
code-labelled on stderr, with no stdout on pre-output failure. JSON contains title,
`sourceUrl`, provider id, ingredients, ordered instructions and optional author,
description, yield and ISO duration metadata. Plain-text output includes attribution
and a reminder to consult the source for numbered notes, substitutions, safety and
allergens; it does not interpret amounts or scale recipes.

## Actual discovery and parsing support

Public pages inspected for the original implementation on **2026-10-10**:

- `https://www.recipetineats.com/?s=chicken` serves ordinary HTML search posts in
  `article .entry-title-link`. Search requests **one page**, at most 20 candidates
  in site order per enabled provider. Candidates may be non-recipe posts; it does
  not fetch/verify each candidate. No pagination, bulk crawling or search API.
- `https://www.recipetineats.com/butter-chicken/` contains a schema.org Recipe
  inside Yoast JSON-LD `@graph`. Retrieval requires a complete structured Recipe
  with title, ingredients and instructions. Root objects/arrays, type arrays,
  `@graph`, HowToStep and nested HowToSection order are handled. Entities and markup
  become plain text; untrusted JSON-LD canonical/step URLs are **not fetched**.

Reduced attributed fixtures from those pages are in `testdata/`; recipe/site
content belongs to Nagi / RecipeTin Eats, not to this repository's MIT license.
No generic prose fallback, JavaScript, challenge solving, impersonation, access
bypass or retry. 403/429/etc are errors. A site layout/challenge can cause EMPTY or
RECIPE_DATA; inspect the original URL rather than bypass its restrictions.

## Persisted provider configuration

`recipetin-eats` is enabled by default. Both implicit and explicit disabled
search/get stop **before invoking the provider, resolving DNS or opening transport**.
Selection does not enable a disabled provider.

Path precedence: `--config FILE` > `RECIPES_CONFIG` >
`$XDG_CONFIG_HOME/recipes-cli/config.json` > `~/.config/recipes-cli/config.json`.
Empty override paths and non-absolute/empty XDG_CONFIG_HOME fail. Relative explicit
paths resolve against the working directory. Missing config means defaults and
status/read commands create nothing. Example:

```json
{"version":1,"providers":{"recipetin-eats":false}}
```

Unknown keys/provider ids, unsupported version, null/non-boolean overrides and
malformed JSON fail without overwrite. Only enable/disable writes configuration;
private (0600) temp files are synced, closed and atomically renamed in the same
directory; new directories are 0700, failed writes remove temp files. **No concurrent
writer lock**: avoid simultaneous edits (last writer can win). No migration is
needed for the same version-1 TypeScript JSON schema, but the Go CLI uses the same
default path: use separate explicit configs when comparing alternatives. Neither
implementation's data/config is modified by builds/tests/live smoke.

## Network boundary and SSRF protections

- HTTPS only, exact provider hostnames (`recipetineats.com`, `www.recipetineats.com`),
  no credentials or custom ports (explicit 443 accepted/normalized). No suffix
  matching, trailing-dot hosts or arbitrary URL/IP access. Fragment removed.
- Initial and **every redirect** URL validated. All DNS answers must pass public
  address policy before the next transport opens. Empty, invalid, scoped, private,
  local, reserved or **mixed public/non-public** answers fail closed.
- `net/netip` strict literals; non-public IPv4-mapped IPv6 cannot bypass IPv4 checks.
  Public mapped IPv4 is unwrapped to its validated IPv4 literal for dialing (the
  positive public-mapped cases requested by the original QA review are retained).
- IPv4 excludes local/private/shared/link-local/loopback, protocol assignments,
  documentation, benchmarking, multicast/reserved and special-purpose services.
  IPv6 accepts only the **34 ordinary ALLOCATED prefixes** in IANA's registry,
  minus special-purpose blocks. It does **not** trust `IsGlobalUnicast` or the
  entire `2000::/3` envelope: allocation gaps and future/reserved space are refused.
  Teredo/6to4/NAT64, site-local, dummy and SRv6 prefixes are refused too.
- A fresh Go transport pins the first validated literal in `DialContext`; no
  transport DNS re-resolution/proxies/connection reuse/HTTP2 coalescing. Original
  hostname remains Host and TLS ServerName, normal certificate validation retained,
  TLS >=1.2. Dial callback checks cancellation, pin and target identity again.
- A single **10-second context deadline** spans DNS, all redirects, headers and
  body. Maximum **3 redirects**, **2 MiB** streamed/declared body cap, UTF-8 HTML and
  identity encoding only. Response bodies close on success/failure/redirect.
  Explicit `recipes-cli/0.1.0-go` user-agent; no retries or address failover.

### Static address policy maintenance

Authoritative sources read **2026-10-10**:
[IANA global IPv6 allocations](https://www.iana.org/assignments/ipv6-unicast-address-assignments/),
[IPv6 special-purpose](https://www.iana.org/assignments/iana-ipv6-special-registry/),
[IPv4 special-purpose](https://www.iana.org/assignments/iana-ipv4-special-registry/).
Unlisted `2000::/3` space is explicitly reserved by IANA. Policy is in
`internal/netfetch/address.go`; independently captured IANA allocation endpoints
and gap endpoints are in `internal/netfetch/testdata/iana-allocations.json`.
The snapshot is static: new allocations fail closed until reviewed; new exclusions
need a policy update. Review all three registries when refreshing, not just sampled
addresses; preserve classification/initial/mixed/redirect and positive pin tests.
Some globally reachable special-service addresses are deliberately unsupported.
This validates address policy, not guaranteed routability or protection against
malicious OS/network routing. NAT64-only networks and proxies are unsupported.

## Tests and optional live check

```sh
go mod download                         # dependency setup; may use network
GOPROXY=off GOSUMDB=off go test ./... -count=1
GOPROXY=off GOSUMDB=off go test -race ./... -count=1
go test ./internal/netfetch -run 'TestPublicAddressPolicy|TestRejectedDNSZeroTransport|TestPinnedNativeTransportNoReresolution' -count=1
go vet ./...
test -z "$(gofmt -l cmd internal tools)"
git diff --check
# Optional, separate, exactly one public search + one recipe (redirects if needed):
go run ./tools/live-smoke
```

Offline tests use static fixtures, mocked DNS/open/body failures and a native
transport fake dial which never opens a socket. They cover original QA IPv6
examples, all 13 allocation gaps and 68 ordinary allocated-prefix endpoints,
non-public mapped IPs, deceptive URLs, mixed DNS, redirect revalidation, pinning,
response closure, limits and aggregate deadlines. In-memory `net.Pipe` TLS 1.2
checks retain original hostname SNI/Host, accept a test-trusted matching certificate
and reject a test-trusted wrong-host certificate without sockets. Parser/config/registry/CLI tests
cover attribution, malformed/missing data, unknown providers, versioned persistence,
no overwrite, disabled zero-invocation paths and two simultaneous synthetic
providers through unchanged CLI handlers. The executable test builds into a temp
directory and executes the native binary with empty PATH/isolated config: no Node,
Go runtime or system install. `-race` requires a C toolchain for instrumentation;
normal `CGO_ENABLED=0` builds do not. CI runs offline checks on Go 1.26 and 1.27;
CI never runs the live smoke.

## Maintainer release process

`.github/workflows/release.yml` validates PRs and `main` pushes with the synthetic
version `v0.0.0`, building packages and running native offline smoke checks, **without
publishing**. Only an intentionally pushed stable `vX.Y.Z` tag can publish; leading
zeroes, prereleases and build suffixes are rejected. There is no publishing dispatch.

After a release commit/PR and its QA have been approved and merged, and **only after
explicit release authorization**, choose a new stable version and tag the reviewed
commit. Illustrative commands (do not run with the placeholder):

```sh
git fetch origin main
git tag -a vX.Y.Z REVIEWED_COMMIT_SHA -m 'Recipes CLI vX.Y.Z'
git push origin refs/tags/vX.Y.Z
```

Pushing the tag starts publication automatically: no later approval prompt is built
into the workflow. Do not push a tag merely to test the workflow. Repository writers
must preserve tags; repository protection/permission settings are not changed here.
All external actions are pinned to reviewed full commit SHAs. Update pins through QA.

Pipeline: validate tag and release-helper tests → offline formatting/module consistency,
`go vet` and race tests on Go 1.26/1.27 → six `CGO_ENABLED=0` cross-builds and validated
archives → native `--version`, `--help` and read-only provider-status smoke on six
hosted runners → revalidate the complete six-package/checksum collection → upload
one draft release with six archives and `SHA256SUMS` → verify draft asset names/sizes
and unchanged remote tag → publish. All test/build jobs are read-only; only the
successful tag-gated publisher has `contents: write`, using the built-in GitHub token.
Dependency/toolchain downloads are setup operations; tests/vet/builds run offline.
The live smoke tool is never executed by CI. Native smoke runs with an empty PATH.

The publisher refuses to replace **any** existing release (including a draft), uses
`--verify-tag`, and checks lightweight/annotated tags resolve to the tested commit
before and after upload. A failed upload/final check may leave a draft, never an
intentionally published partial release. Inspect failed run logs and any draft;
do not blindly re-run or delete a published release/tag. Recovering a failed draft
requires an authorized maintainer decision. This workflow never auto-deletes assets,
uses `--clobber`, or changes repository settings. A normal successful rerun fails
closed because the release already exists. Tag runs are serialized per ref.

Local offline helper checks after dependency setup:

```sh
python3 -m unittest discover -s .github/scripts -p 'test_*.py' -v
python3 .github/scripts/release.py build --tag v1.2.3 --os linux --arch amd64 --dir dist
# Optional native smoke, only for your actual OS/architecture:
python3 .github/scripts/release.py smoke --tag v1.2.3 --os linux --arch amd64 --dir dist
# `verify` requires all six archives and per-archive .sha256 files in the directory.
```

## Limitations and rollback

First-page candidates only; requires complete Recipe JSON-LD. Notes, ingredient/
step section grouping, images/video/nutrition are not reproduced; consult source.
UTF-8/identity only, no compression/proxies/retries, first public DNS address only.
OS DNS cancellation depends on Go/OS resolver, but cancelled/late resolution is
checked before opening transport; default Go HTTP honors context during TLS/body.
Static IP policy may reject newly allocated/special networks. Config writes are
atomic but unlocked. No other production providers/dynamic plugins/auth/cloud/
meal-planning/bulk discovery. Live checks cover two pages, not future layouts.
Deterministic attacks are mocks/fake dial, not internal-network probes or exhaustive
native TLS/OS integration. Linux cross-build is not proof of Linux execution.

No daemon/deployment/database/repository-permission changes. Roll back a local install
by removing/restoring the copied native binary. Config is external and not removed
on rollback; preserve it, or deliberately move it aside to reset defaults. For
comparison with TypeScript keep distinct binaries/config paths to avoid overwriting
an existing `recipes` installation. Nothing here merges or closes PR #2.

## Add a second built-in provider (no CLI-handler changes)

Implement `provider.Provider` in a separate `internal/NEWPROVIDER` package: `Info`
(id/name/exact hosts/default), `Matches`, `Search`, `Get`. Return normalized
`model` values with attribution and call the injected `netfetch.Fetch` with that
provider's exact allowlist; never fetch directly. Register it in the default
`provider.New(...)` call in `internal/cli/cli.go` (registration only, not handlers).
Add attributed fixtures/parser/URL tests and registry/CLI enable/disable/dispatch
coverage. Registry aggregates enabled search results sequentially in registration
order and routes retrieval by host; a selected provider restricts both. Existing
synthetic two-provider tests demonstrate extension with no command-specific logic.
This v1 registers only RecipeTin Eats; dynamic plugin installation is out of scope.
