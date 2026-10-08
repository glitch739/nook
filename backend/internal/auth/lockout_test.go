package auth

import (
	"testing"
	"time"
)

func TestAccountGuardBackoff(t *testing.T) {
	g := NewAccountGuard()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		if lock := g.Fail("Alice"); lock != 0 {
			t.Fatalf("failure %d: locked %v within the free failures", i+1, lock)
		}
	}
	if locked, _ := g.Locked("alice"); locked {
		t.Fatal("locked after only the free failures")
	}

	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if got := g.Fail(" ALICE "); got != w {
			t.Fatalf("over-limit failure %d: lock %v, want %v", i+1, got, w)
		}
	}
	if locked, wait := g.Locked("alice"); !locked || wait != 30*time.Minute {
		t.Fatalf("Locked = %v, %v", locked, wait)
	}

	now = now.Add(31 * time.Minute)
	if locked, _ := g.Locked("alice"); locked {
		t.Fatal("still locked after the lock expired")
	}

	g.Succeed("alice")
	if lock := g.Fail("alice"); lock != 0 {
		t.Fatalf("success didn't reset the record, lock %v", lock)
	}
}

func TestAccountGuardForgetsAndBounds(t *testing.T) {
	g := NewAccountGuard()
	g.MaxEntries = 3
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		g.Fail("bob")
	}
	now = now.Add(25 * time.Hour)
	if lock := g.Fail("bob"); lock != 0 {
		t.Fatalf("old failures weren't forgotten, lock %v", lock)
	}

	for _, n := range []string{"a", "b", "c", "d", "e"} {
		g.Fail(n)
		now = now.Add(time.Second)
	}
	if len(g.entries) > 3 {
		t.Fatalf("%d entries, cap is 3", len(g.entries))
	}
}

func TestAccountGuardNilIsNoop(t *testing.T) {
	var g *AccountGuard
	g.Fail("x")
	g.Succeed("x")
	if locked, _ := g.Locked("x"); locked {
		t.Fatal("nil guard locked")
	}
}
