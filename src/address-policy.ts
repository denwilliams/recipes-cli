import { isIP } from 'node:net';
import ipaddr from 'ipaddr.js';

// Static fail-closed snapshot: only IANA ALLOCATED prefixes designated to RIRs.
// Source: https://www.iana.org/assignments/ipv6-unicast-address-assignments/
// Registry updated 2025-10-10, inspected 2026-10-10. Unlisted space inside
// 2000::/3 is RESERVED too: the envelope is not itself a public allocation.
// No IANA protocol/transition blocks, special-service exceptions or future
// allocations are automatically permitted. See fixture for the complete table.
const allocatedV6 = [
  '2001:200::/23', // APNIC
  '2001:400::/23', // ARIN
  '2001:600::/23', // RIPE NCC
  '2001:800::/22', // RIPE NCC
  '2001:c00::/23', // APNIC
  '2001:e00::/23', // APNIC
  '2001:1200::/23', // LACNIC
  '2001:1400::/22', // RIPE NCC
  '2001:1800::/23', // ARIN
  '2001:1a00::/23', // RIPE NCC
  '2001:1c00::/22', // RIPE NCC
  '2001:2000::/19', // RIPE NCC
  '2001:4000::/23', // RIPE NCC
  '2001:4200::/23', // AFRINIC
  '2001:4400::/23', // APNIC
  '2001:4600::/23', // RIPE NCC
  '2001:4800::/23', // ARIN
  '2001:4a00::/23', // RIPE NCC
  '2001:4c00::/23', // RIPE NCC
  '2001:5000::/20', // RIPE NCC
  '2001:8000::/19', // APNIC
  '2001:a000::/20', // APNIC
  '2001:b000::/20', // APNIC
  '2003::/18', // RIPE NCC
  '2400::/12', // APNIC
  '2410::/12', // APNIC
  '2600::/12', // ARIN
  '2610::/23', // ARIN
  '2620::/23', // ARIN
  '2630::/12', // ARIN
  '2800::/12', // LACNIC
  '2a00::/12', // RIPE NCC
  '2a10::/12', // RIPE NCC
  '2c00::/12', // AFRINIC
].map((cidr) => ipaddr.IPv6.parseCIDR(cidr));
const excludedV6 = [
  '2001::/23',       // Protocol assignments, including Teredo, benchmarking, ORCHID
  '2001:db8::/32',   // Documentation
  '2002::/16',       // 6to4 (can encapsulate non-public IPv4)
  '2620:4f:8000::/48', // Special-purpose AS112 service
  '3ffe::/16',       // Former 6bone allocation, returned/reserved
  '3fff::/20',       // Documentation (RFC 9637)
].map((cidr) => ipaddr.IPv6.parseCIDR(cidr));

export function isPublicAddress(address: string): boolean {
  try {
    if (address.includes('%') || !isIP(address)) return false;
    // process converts IPv4-mapped IPv6 to IPv4; apply the same IPv4 exclusions.
    const parsed = ipaddr.process(address);
    if (parsed instanceof ipaddr.IPv4) return parsed.range() === 'unicast';
    // Positive allocation matching refuses local/special/unallocated space,
    // including all gaps inside 2000::/3. Mapped IPv4 was handled above.
    return allocatedV6.some((cidr) => parsed.match(cidr)) && !excludedV6.some((cidr) => parsed.match(cidr));
  } catch { return false; }
}

