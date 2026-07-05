package main

import (
	"testing"

	"github.com/Haroutio/arrsenal/internal/state"
	"github.com/Haroutio/arrsenal/internal/wire"
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

func TestResolveUsenetProvider(t *testing.T) {
	// Missing any of the trio → nil: never register a half-configured server.
	if p := resolveUsenetProvider(options{usenetProvider: "newshosting", usenetUser: "u"}); p != nil {
		t.Fatalf("missing password must resolve to nil, got %+v", p)
	}

	// A preset fills host/port/ssl/connections.
	p := resolveUsenetProvider(options{usenetProvider: "Newshosting", usenetUser: "u", usenetPass: "pw"})
	if p == nil || p.Host != "news.newshosting.com" || p.Port != 563 || !p.SSL || p.Connections != 30 {
		t.Fatalf("preset not applied: %+v", p)
	}

	// A hostname is a custom provider on the standard TLS port, named
	// after its host in SAB's server list.
	p = resolveUsenetProvider(options{usenetProvider: "news.example.net", usenetUser: "u", usenetPass: "pw"})
	if p == nil || p.Host != "news.example.net" || p.Port != 563 || p.Connections != 20 || p.Name != "news.example.net" {
		t.Fatalf("custom host not applied: %+v", p)
	}

	// Explicit overrides win.
	p = resolveUsenetProvider(options{usenetProvider: "eweka", usenetUser: "u", usenetPass: "pw",
		usenetPort: 119, usenetConnections: 8})
	if p == nil || p.Port != 119 || p.Connections != 8 {
		t.Fatalf("overrides not applied: %+v", p)
	}
}

func TestResolveUsenetProvidersMergesPromptAndFlag(t *testing.T) {
	// The prompt loops (backup and block accounts are normal); the flag
	// path contributes one more.
	o := options{
		usenetProviders: []wire.UsenetProvider{
			buildUsenetProvider("newshosting", "u1", "p1", 0, 0),
			buildUsenetProvider("news.customhost.example", "u2", "p2", 0, 50),
		},
		usenetProvider: "eweka", usenetUser: "u3", usenetPass: "p3",
	}
	got := resolveUsenetProviders(o)
	if len(got) != 3 {
		t.Fatalf("want 3 providers, got %d: %+v", len(got), got)
	}
	if got[0].Host != "news.newshosting.com" || got[1].Host != "news.customhost.example" || got[2].Host != "news.eweka.nl" {
		t.Fatalf("hosts wrong: %+v", got)
	}
	if got[1].Name != "news.customhost.example" || got[1].Connections != 50 {
		t.Fatalf("custom provider shape wrong: %+v", got[1])
	}

	// No providers anywhere → empty, not nil-panic.
	if got := resolveUsenetProviders(options{}); len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}
