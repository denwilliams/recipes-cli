import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ipaddr from 'ipaddr.js';
import { createFetcher, isPublicAddress, type HttpResponse } from '../src/http.js';
import { RecipesError } from '../src/errors.js';

const url = 'https://www.recipetineats.com/recipe/';
const hosts = ['www.recipetineats.com'];
const publicAnswer = { address: '2606:4700::1111', family: 6 };
const refused = [
  '2d00::1', '2e00::1', '3000::1', '3800::1', '3c00::1', '3e00::1', '3f00::1',
  '3f80::1', '3fc0::1', '3fe0::1', '3ff0::1', '3ff8::1', '3ffc::1', '3fff:1000::1',
];
for (const address of refused) {
  test(`IANA reserved ${address} classifier refuses`, () => assert.equal(isPublicAddress(address), false));
  for (const mode of ['initial', 'mixed-first', 'mixed-last', 'redirect', 'redirect-mixed'] as const) {
    test(`IANA reserved ${address}: ${mode} DNS cannot reach transport`, async () => {
      const answer = { address, family: 6 };
      const addresses = mode === 'mixed-first' ? [answer, publicAnswer]
        : mode === 'mixed-last' || mode === 'redirect-mixed' ? [publicAnswer, answer] : [answer];
      const redirect = mode.startsWith('redirect');
      let lookups = 0, opens = 0, closes = 0;
      const fetchPage = createFetcher({
        resolve: async () => ++lookups === 1 && redirect ? [publicAnswer] : addresses,
        open: async (): Promise<HttpResponse> => {
          opens++;
          return {
            status: redirect ? 302 : 200,
            headers: redirect ? { location: '/final/' } : { 'content-type': 'text/html' },
            body: (async function* () { yield Buffer.from('x'); })(), close() { closes++; },
          };
        },
      });
      await assert.rejects(fetchPage(url, hosts), (e) => e instanceof RecipesError && e.code === 'NETWORK');
      assert.equal(opens, redirect ? 1 : 0);
      assert.equal(lookups, redirect ? 2 : 1);
      assert.equal(closes, opens);
    });
  }
}

// Independent static registry snapshot, not imported from the runtime policy.
const snapshot = JSON.parse(readFileSync('test/fixtures/iana-global-unicast.json', 'utf8')) as {
  entries: { prefix: string; designation: string; status: string }[];
};
const isRir = (entry: typeof snapshot.entries[number]) =>
  entry.status === 'ALLOCATED' && ['APNIC', 'ARIN', 'RIPE NCC', 'LACNIC', 'AFRINIC'].includes(entry.designation);
function integer(address: ipaddr.IPv6): bigint {
  return address.toByteArray().reduce((value, byte) => (value << 8n) | BigInt(byte), 0n);
}
function literal(value: bigint): string {
  return Array.from({ length: 8 }, (_, i) => ((value >> BigInt((7 - i) * 16)) & 0xffffn).toString(16)).join(':');
}
function endpoints(prefix: string): [bigint, bigint] {
  const [address, length] = ipaddr.IPv6.parseCIDR(prefix);
  const start = integer(address);
  return [start, start + (1n << BigInt(128 - length)) - 1n];
}
for (const entry of snapshot.entries) {
  const expected = isRir(entry);
  test(`registry ${entry.status} ${entry.designation} ${entry.prefix}: inclusive first/last policy`, () => {
    for (const value of endpoints(entry.prefix)) assert.equal(isPublicAddress(literal(value)), expected);
  });
}
// Check both ends of EVERY gap in the global envelope, including unlisted space;
// adjacent allocations produce no gap. This guards against another broad-envelope fix.
const allocated = snapshot.entries.filter(isRir).map((entry) => endpoints(entry.prefix)).sort((a, b) => a[0] < b[0] ? -1 : 1);
const [envelopeStart, envelopeEnd] = endpoints('2000::/3');
let cursor = envelopeStart;
for (const [start, end] of [...allocated, [envelopeEnd + 1n, envelopeEnd + 1n] as [bigint, bigint]]) {
  if (cursor < start) {
    const gapStart = cursor, gapEnd = start - 1n;
    test(`unallocated IPv6 gap ${literal(gapStart)}..${literal(gapEnd)} is refused`, () => {
      assert.equal(isPublicAddress(literal(gapStart)), false);
      assert.equal(isPublicAddress(literal(gapEnd)), false);
    });
  }
  cursor = end + 1n;
}

test('special prefixes within RIR allocations still fail closed and public neighbors are admitted', () => {
  for (const address of ['2001:db8::1', '2001:db8:ffff:ffff:ffff:ffff:ffff:ffff', '2620:4f:8000::1', '2620:4f:8000:ffff:ffff:ffff:ffff:ffff']) {
    assert.equal(isPublicAddress(address), false, address);
  }
  for (const address of ['2001:db7:ffff:ffff:ffff:ffff:ffff:ffff', '2001:db9::1', '2620:4f:7fff:ffff:ffff:ffff:ffff:ffff', '2620:4f:8001::1', '2410::1', '2a10::1', '2001:2000::1']) {
    assert.equal(isPublicAddress(address), true, address);
  }
});
