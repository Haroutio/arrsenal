package main

import (
	"testing"

	"github.com/Haroutio/arrsenal/internal/state"
)

func TestParseGPUAnswer(t *testing.T) {
	cases := []struct {
		in   string
		want state.GPUMode
	}{
		{"nvidia", state.GPUNvidia},
		{" Intel \n", state.GPUIntel}, // trimmed and case-folded
		{"AMD", state.GPUAMD},
		{"", state.GPUNone},
		{"\n", state.GPUNone},
		{"none", state.GPUNone},
		{"voodoo2", state.GPUNone}, // unknown answers fail safe, never invalid state
	}
	for _, tc := range cases {
		if got := parseGPUAnswer(tc.in); got != tc.want {
			t.Errorf("parseGPUAnswer(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestLanIPReturnsSomethingUsable(t *testing.T) {
	if lanIP() == "" {
		t.Fatal("lanIP must always produce a non-empty host")
	}
}
