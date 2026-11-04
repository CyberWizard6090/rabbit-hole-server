package safehttp

import (
	"net"
	"net/netip"
)

var blockedIPv4 = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
)

var globalUnicastIPv6 = netip.MustParsePrefix("2000::/3")

var blockedIPv6 = mustPrefixes(
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"3fff::/20",
)

var loopbackPrefixes = mustPrefixes("127.0.0.0/8", "::1/128")

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, v := range values {
		out = append(out, netip.MustParsePrefix(v))
	}
	return out
}

func containsAny(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func normalize(addr netip.Addr) netip.Addr {
	if addr.Is4In6() {
		return addr.Unmap()
	}
	return addr
}

func IsPublicAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = normalize(addr)
	if addr.Is4() {
		return !containsAny(blockedIPv4, addr)
	}
	if !globalUnicastIPv6.Contains(addr) {
		return false
	}
	return !containsAny(blockedIPv6, addr)
}

func IsPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	return IsPublicAddr(addr)
}

func isLoopback(addr netip.Addr) bool {
	return containsAny(loopbackPrefixes, normalize(addr))
}
