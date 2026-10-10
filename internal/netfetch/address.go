package netfetch

import "net/netip"

func prefixes(items ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(items))
	for i, s := range items {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// Static conservative policy from IANA registries read 2026-10-10. See README.
// ALLOCATED ordinary global-unicast prefixes only (not all of 2000::/3).
var allocatedV6 = prefixes(
	"2001:200::/23", "2001:400::/23", "2001:600::/23", "2001:800::/22",
	"2001:c00::/23", "2001:e00::/23", "2001:1200::/23", "2001:1400::/22",
	"2001:1800::/23", "2001:1a00::/23", "2001:1c00::/22", "2001:2000::/19",
	"2001:4000::/23", "2001:4200::/23", "2001:4400::/23", "2001:4600::/23",
	"2001:4800::/23", "2001:4a00::/23", "2001:4c00::/23", "2001:5000::/20",
	"2001:8000::/19", "2001:a000::/20", "2001:b000::/20", "2003::/18",
	"2400::/12", "2410::/12", "2600::/12", "2610::/23", "2620::/23",
	"2630::/12", "2800::/12", "2a00::/12", "2a10::/12", "2c00::/12",
)
var excludedV6 = prefixes("2001::/23", "2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3ffe::/16", "3fff::/20")

// Deny all special-purpose IPv4, including globally reachable special services,
// and multicast/reserved. This deliberately trades special-service compatibility for safety.
var excludedV4 = prefixes("0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.31.196.0/24",
	"192.52.193.0/24", "192.88.99.0/24", "192.168.0.0/16", "192.175.48.0/24", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4")

func contains(ps []netip.Prefix, a netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// IsPublicAddress accepts strict IP literals only. Mapped IPv4 is checked as IPv4;
// private mapped addresses cannot evade the policy. Zones are never accepted.
func IsPublicAddress(a netip.Addr) bool {
	if !a.IsValid() || a.Zone() != "" {
		return false
	}
	a = a.Unmap()
	if a.Is4() {
		return !contains(excludedV4, a)
	}
	return contains(allocatedV6, a) && !contains(excludedV6, a)
}
