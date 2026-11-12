package collect

import (
	"strconv"
	"testing"
)

func TestDecodeProcNetAddrIPv4(t *testing.T) {
	// 10.0.0.5:22 — little-endian hex as in /proc/net/tcp
	ip, port, ok := decodeProcNetAddr("0500000A:0016")
	if !ok {
		t.Fatal("expected ok")
	}
	if ip != "10.0.0.5" || port != 22 {
		t.Fatalf("got %s:%d", ip, port)
	}
}

func TestDecodeProcNetAddrIPv6Localhost(t *testing.T) {
	// ::1 port 80
	ip, port, ok := decodeProcNetAddr("00000000000000000000000001000000:0050")
	if !ok {
		t.Fatal("expected ok")
	}
	if port != 80 {
		t.Fatalf("port=%d", port)
	}
	if ip != "::1" && ip != "0:0:0:0:0:0:0:1" {
		t.Fatalf("ip=%q", ip)
	}
}

func TestIsProcNetRemoteUnspecified(t *testing.T) {
	if !isProcNetRemoteUnspecified("00000000:0000") {
		t.Fatal("ipv4 zero remote")
	}
	if !isProcNetRemoteUnspecified("00000000000000000000000000000000:0000") {
		t.Fatal("ipv6 zero remote")
	}
	if isProcNetRemoteUnspecified("0100007F:0035") {
		t.Fatal("should not treat non-zero remote as unspecified")
	}
}

func TestNormalizeListenIP(t *testing.T) {
	cases := map[string]string{
		"":                   "0.0.0.0",
		"127.0.0.1":          "127.0.0.1",
		"::ffff:127.0.0.1":   "127.0.0.1",
		"::1":                "::1",
		"0.0.0.0":            "0.0.0.0",
		"  ::ffff:10.0.0.1 ": "10.0.0.1",
	}
	for in, want := range cases {
		if got := normalizeListenIP(in); got != want {
			t.Fatalf("normalizeListenIP(%q)=%q want %q", in, got, want)
		}
	}
}

func TestLookupServiceDoesNotPanic(t *testing.T) {
	_ = lookupService("tcp", 22)
	_ = lookupService("udp", 53)
	_ = lookupService("tcp", 0)
}

func TestCollectListenersShape(t *testing.T) {
	out := collectListeners()
	if out == nil {
		t.Fatalf("collectListeners returned nil")
	}
	seen := map[string]struct{}{}
	for i, l := range out {
		if l.Protocol != "tcp" && l.Protocol != "udp" {
			t.Fatalf("entry %d bad protocol %q", i, l.Protocol)
		}
		if l.Port < 1 {
			t.Fatalf("entry %d bad port %d", i, l.Port)
		}
		key := l.Protocol + "|" + l.IP + "|" + strconv.Itoa(int(l.Port))
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate listener %s", key)
		}
		seen[key] = struct{}{}
		if i == 0 {
			continue
		}
		prev := out[i-1]
		if prev.Protocol > l.Protocol {
			t.Fatalf("not sorted by protocol: %#v then %#v", prev, l)
		}
		if prev.Protocol == l.Protocol && prev.Port > l.Port {
			t.Fatalf("not sorted by port: %#v then %#v", prev, l)
		}
		if prev.Protocol == l.Protocol && prev.Port == l.Port && prev.IP > l.IP {
			t.Fatalf("not sorted by ip: %#v then %#v", prev, l)
		}
	}
}
