package main

import "testing"

func TestVersionDefaultsToDev(t *testing.T) {
	// Release builds stamp version via ldflags; anything built without them
	// must self-identify as a dev build.
	if version != "dev" {
		t.Fatalf("version = %q, want %q in non-release builds", version, "dev")
	}
}
