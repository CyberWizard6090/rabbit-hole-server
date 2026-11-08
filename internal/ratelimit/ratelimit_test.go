package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestWindowCapsPerKeyAndResets(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindow(2, time.Minute).WithClock(func() time.Time { return clock })

	for i := 0; i < 2; i++ {
		if ok, _ := w.Allow("a"); !ok {
			t.Fatalf("request %d must pass", i+1)
		}
	}
	ok, wait := w.Allow("a")
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("third request: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := w.Allow("b"); !ok {
		t.Fatal("another key has its own budget")
	}
	clock = clock.Add(time.Minute + time.Second)
	if ok, _ := w.Allow("a"); !ok {
		t.Fatal("budget must reset after the window")
	}
}

func TestWindowDisabledAndNil(t *testing.T) {
	var nilWindow *Window
	if ok, _ := nilWindow.Allow("x"); !ok {
		t.Fatal("nil window allows everything")
	}
	if ok, _ := NewWindow(0, time.Minute).Allow("x"); !ok {
		t.Fatal("zero max disables the limit")
	}
}

func TestWindowStateStaysBounded(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindow(1, time.Hour).WithMaxKeys(50).WithClock(func() time.Time { return clock })
	for i := 0; i < 1000; i++ {
		w.Allow(fmt.Sprintf("key-%d", i))
		if w.Len() > 50 {
			t.Fatalf("window holds %d keys, limit is 50", w.Len())
		}
	}
}

func TestWindowSweepsExpiredKeys(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w := NewWindow(1, time.Minute).WithClock(func() time.Time { return clock })
	for i := 0; i < 20; i++ {
		w.Allow(fmt.Sprintf("key-%d", i))
	}
	clock = clock.Add(3 * time.Minute)
	w.Allow("fresh")
	if w.Len() != 1 {
		t.Fatalf("expired keys were not swept: %d left", w.Len())
	}
}

func TestClientKeyGroupsIPv6ByPrefix(t *testing.T) {
	cases := map[string]string{
		"203.0.113.9":                      "203.0.113.9",
		"::ffff:203.0.113.9":               "203.0.113.9",
		"2001:db8:1:2:aaaa:bbbb:cccc:dddd": "2001:db8:1:2::/64",
		"2001:db8:1:2::1":                  "2001:db8:1:2::/64",
		"2001:db8:1:3::1":                  "2001:db8:1:3::/64",
		"not-an-ip":                        "not-an-ip",
	}
	for in, want := range cases {
		if got := ClientKey(in); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", in, got, want)
		}
	}
	if ClientKey("2001:db8:1:2:1::1") != ClientKey("2001:db8:1:2:ffff::2") {
		t.Fatal("addresses in one /64 must share a key")
	}
	if ClientKey("203.0.113.9") == ClientKey("203.0.113.10") {
		t.Fatal("IPv4 addresses are keyed individually")
	}
}

func TestBackoffLocksAfterThresholdAndGrowsExponentially(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := NewBackoff(BackoffConfig{Threshold: 3, Base: 10 * time.Second, Max: 35 * time.Second})
	b.now = func() time.Time { return clock }

	for i := 0; i < 2; i++ {
		if delay := b.Fail("user@example.test"); delay != 0 {
			t.Fatalf("failure %d must not lock yet", i+1)
		}
	}
	if delay := b.Fail("user@example.test"); delay != 10*time.Second {
		t.Fatalf("third failure delay = %v", delay)
	}
	if locked, wait := b.Locked("user@example.test"); !locked || wait != 10*time.Second {
		t.Fatalf("locked=%v wait=%v", locked, wait)
	}
	clock = clock.Add(11 * time.Second)
	if locked, _ := b.Locked("user@example.test"); locked {
		t.Fatal("the lock must expire")
	}
	if delay := b.Fail("user@example.test"); delay != 20*time.Second {
		t.Fatalf("fourth failure delay = %v", delay)
	}
	clock = clock.Add(21 * time.Second)
	if delay := b.Fail("user@example.test"); delay != 35*time.Second {
		t.Fatalf("delay must be capped, got %v", delay)
	}
	if locked, _ := b.Locked("other@example.test"); locked {
		t.Fatal("other subjects are not affected")
	}
}

func TestBackoffSucceedResetsAndForgetsOldFailures(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := NewBackoff(BackoffConfig{Threshold: 2, Base: time.Minute, Max: time.Hour, Forget: time.Hour})
	b.now = func() time.Time { return clock }

	b.Fail("a")
	b.Fail("a")
	b.Succeed("a")
	if locked, _ := b.Locked("a"); locked {
		t.Fatal("success must clear the lock")
	}
	b.Fail("a")
	clock = clock.Add(2 * time.Hour)
	if delay := b.Fail("a"); delay != 0 {
		t.Fatal("old failures must be forgotten")
	}
}

func TestBackoffDisabledAndBounded(t *testing.T) {
	var nilBackoff *Backoff
	nilBackoff.Fail("a")
	nilBackoff.Succeed("a")
	if locked, _ := nilBackoff.Locked("a"); locked {
		t.Fatal("nil backoff never locks")
	}
	if NewBackoff(BackoffConfig{}).Enabled() {
		t.Fatal("threshold 0 disables the lock")
	}

	b := NewBackoff(BackoffConfig{Threshold: 1, Base: time.Hour, Max: time.Hour, MaxKeys: 20})
	for i := 0; i < 500; i++ {
		b.Fail(fmt.Sprintf("subject-%d", i))
		if b.Len() > 20 {
			t.Fatalf("backoff holds %d entries, limit is 20", b.Len())
		}
	}
}
