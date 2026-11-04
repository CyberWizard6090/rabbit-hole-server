package safehttp

import (
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestIsPublicAddr(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"93.184.216.34", true},
		{"172.15.255.255", true},
		{"172.32.0.1", true},
		{"100.63.255.255", true},
		{"100.128.0.1", true},
		{"2606:4700:4700::1111", true},
		{"2001:4860:4860::8888", true},
		{"::ffff:8.8.8.8", true},

		{"0.0.0.0", false},
		{"0.1.2.3", false},
		{"127.0.0.1", false},
		{"127.255.255.254", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"169.254.0.1", false},
		{"100.64.0.1", false},
		{"100.127.255.255", false},
		{"224.0.0.1", false},
		{"239.255.255.250", false},
		{"240.0.0.1", false},
		{"255.255.255.255", false},
		{"192.0.0.1", false},
		{"192.0.2.10", false},
		{"198.18.0.1", false},
		{"198.19.255.255", false},
		{"198.51.100.7", false},
		{"203.0.113.9", false},
		{"192.88.99.1", false},

		{"::", false},
		{"::1", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"fd12:3456::1", false},
		{"ff02::1", false},
		{"2001:db8::1", false},
		{"2002:7f00:1::1", false},
		{"2001::1", false},
		{"64:ff9b::7f00:1", false},
		{"::127.0.0.1", false},
		{"::8.8.8.8", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.1", false},
		{"::ffff:169.254.169.254", false},
		{"::ffff:0:127.0.0.1", false},
		{"fe80::1%eth0", false},
	}
	for _, tc := range cases {
		addr, err := netip.ParseAddr(tc.ip)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.ip, err)
		}
		if got := IsPublicAddr(addr); got != tc.want {
			t.Errorf("IsPublicAddr(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	if !IsPublicIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 should be public")
	}
	if IsPublicIP(net.ParseIP("127.0.0.1")) || IsPublicIP(net.ParseIP("::ffff:127.0.0.1")) {
		t.Error("loopback must not be public")
	}
	if IsPublicIP(nil) || IsPublicIP(net.IP{1, 2, 3}) {
		t.Error("invalid addresses must not be public")
	}
	if IsPublicIP(net.IPv4(10, 1, 2, 3).To16()) {
		t.Error("the 16 byte form of a private address must not be public")
	}
}

func TestValidateURL(t *testing.T) {
	c := New()
	good := []string{
		"https://example.com/a.png",
		"http://example.com/a.png?x=1",
		"https://example.com:443/a.png",
		"http://example.com:80/a.png",
		"https://example.com/a.png#frag",
		"https://example.com./a.png",
		"https://cdn.example.com/a%20b",
		"https://8.8.8.8/a.png",
		"https://[2606:4700:4700::1111]/a.png",
	}
	for _, raw := range good {
		if _, err := c.ValidateURL(raw); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want ok", raw, err)
		}
	}
	bad := []string{
		"",
		"ftp://example.com/a.png",
		"file:///etc/passwd",
		"gopher://example.com/",
		"javascript:alert(1)",
		"data:image/png;base64,AAAA",
		"//example.com/a.png",
		"example.com/a.png",
		"https:///a.png",
		"https://user@example.com/a.png",
		"https://user:pass@example.com/a.png",
		"https://example.com:8080/a.png",
		"https://example.com:22/",
		"https://example.com:0/",
		"https://example.com:99999/",
		"http://localhost/a.png",
		"http://localhost./a.png",
		"http://LOCALHOST/a.png",
		"http://foo.localhost/a.png",
		"http://printer.local/a.png",
		"http://db.internal/a.png",
		"http://127.0.0.1/",
		"http://0x7f.1/",
		"http://0x7f000001/",
		"http://2130706433/",
		"http://017700000001/",
		"http://127.1/",
		"http://1.2.3/",
		"http://example.123/",
		"http://example.0x1f/",
		"http://[::1]/",
		"http://[::ffff:127.0.0.1]/",
		"http://[fe80::1%25eth0]/",
		"http://exa mple.com/",
		"http://example.com\\@evil.com/",
		"https://exa\x00mple.com/",
		"https://münchen.de/a.png",
		"https://example.com/" + string(make([]byte, 0)) + "\n",
		"https://example.com/" + repeat("a", MaxURLLength),
	}
	for _, raw := range bad {
		_, err := c.ValidateURL(raw)
		if raw == "http://127.0.0.1/" || raw == "http://[::1]/" || raw == "http://[::ffff:127.0.0.1]/" {
			if err != nil {
				t.Errorf("literal %q is judged at dial time, got %v", raw, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("ValidateURL(%q) accepted", raw)
		}
	}
}

func TestLoopbackLiteralsAreRefusedAtDial(t *testing.T) {
	c := New()
	for _, raw := range []string{
		"http://127.0.0.1/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/", "http://[fd00::1]/", "http://0.0.0.0/", "http://100.64.0.1/",
	} {
		if _, err := c.Fetch(t.Context(), raw, 1024, ""); !errors.Is(err, ErrForbiddenAddress) {
			t.Errorf("Fetch(%q) = %v, want ErrForbiddenAddress", raw, err)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
