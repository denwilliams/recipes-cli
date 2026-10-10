# Offline fixture provenance

Inspected and reduced on 2026-10-10:
- `https://www.recipetineats.com/butter-chicken/`: the Recipe JSON-LD node inside Yoast's `@graph`. Kept fields used by the parser, removed reviews, images and unrelated nodes. Recipe by Nagi / RecipeTin Eats.
- `https://www.recipetineats.com/?s=chicken`: first two `article .entry-title-link` elements; removed everything else. Real page had 20 candidate posts and pagination.

These are static offline test data, not a promise of stable search ordering or continued availability. Recipe/site content belongs to its source; the repository MIT license does not relicense third-party recipes. Other variants in tests are synthetic.

- `https://www.iana.org/assignments/ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.xhtml`:
  `iana-global-unicast.json` contains the registry prefix/designation/status table, inspected
  2026-10-10 (registry last updated 2025-10-10). Captured independently of the runtime allowlist.
  IANA explicitly reserves unlisted space inside `2000::/3`; tests check both ends of every gap.
