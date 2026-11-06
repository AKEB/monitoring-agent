package collect

import (
	"strconv"
	"testing"
)

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
