package main

import (
	"os"
	"testing"
)

func TestParseFlagsDefaults(t *testing.T) {
	o := parseFlags([]string{}, os.Stdout)
	if o == nil {
		t.Fatal("plain invocation must return options")
	}
	if o.statePath != "/opt/arrsenal/arrsenal.yaml" {
		t.Fatalf("state path default = %q", o.statePath)
	}
	if o.artifactsDir != "/opt/arrsenal" {
		t.Fatalf("artifacts dir must default beside the state file, got %q", o.artifactsDir)
	}
	if o.yes {
		t.Fatal("interactive by default")
	}
	if o.tz == "" {
		t.Fatal("tz must have a detected default")
	}
}

func TestParseFlagsHeadless(t *testing.T) {
	o := parseFlags([]string{
		"--yes", "--apps", "sonarr,sabnzbd",
		"--state", "/tmp/x/arrsenal.yaml",
		"--gpu", "none",
	}, os.Stdout)
	if !o.yes || o.apps != "sonarr,sabnzbd" || o.gpu != "none" {
		t.Fatalf("parsed: %+v", o)
	}
	if o.artifactsDir != "/tmp/x" {
		t.Fatalf("artifacts dir should follow the state file: %q", o.artifactsDir)
	}
}

func TestParseFlagsVersionShortCircuits(t *testing.T) {
	if o := parseFlags([]string{"--version"}, os.NewFile(0, os.DevNull)); o != nil {
		t.Fatal("--version must not proceed to run")
	}
}
