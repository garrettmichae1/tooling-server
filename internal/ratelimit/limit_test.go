package ratelimit

import (
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	lim := New(2)
	now := time.Now()
	if !lim.Allow(now) || !lim.Allow(now) {
		t.Fatal("first calls should pass")
	}
	if lim.Allow(now) {
		t.Fatal("third call in the same minute should fail")
	}
	if !lim.Allow(now.Add(time.Minute + time.Second)) {
		t.Fatal("window should reset")
	}
}
