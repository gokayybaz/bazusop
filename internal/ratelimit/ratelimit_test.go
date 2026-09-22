package ratelimit_test

import (
	"testing"
	"time"

	"github.com/gokayybaz/bazusop/internal/ratelimit"
)

func TestAllowPermitsUpToMaxThenBlocks(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !limiter.Allow("key-a") {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
	}
	if limiter.Allow("key-a") {
		t.Fatal("expected the 4th attempt within the window to be blocked")
	}
}

func TestAllowTracksKeysIndependently(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.New(1, time.Minute)
	if !limiter.Allow("key-a") {
		t.Fatal("expected the first attempt for key-a to be allowed")
	}
	if !limiter.Allow("key-b") {
		t.Fatal("expected key-b's own limit to be independent of key-a's")
	}
	if limiter.Allow("key-a") {
		t.Fatal("expected key-a to still be blocked")
	}
}

func TestAllowResetsAfterTheWindowElapses(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	limiter := ratelimit.New(1, time.Minute, ratelimit.WithClock(clock))
	if !limiter.Allow("key-a") {
		t.Fatal("expected the first attempt to be allowed")
	}
	if limiter.Allow("key-a") {
		t.Fatal("expected the second attempt to be blocked within the window")
	}
	current = current.Add(2 * time.Minute)
	if !limiter.Allow("key-a") {
		t.Fatal("expected a new attempt to be allowed once the window has elapsed")
	}
}
