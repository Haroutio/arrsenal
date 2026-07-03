package main

import (
	"os"
	"strings"
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

func TestNonTTYWithoutYesFailsWithInstructions(t *testing.T) {
	// go test runs without a TTY on stdin — exactly the scripted case.
	o := parseFlags([]string{}, os.Stdout)
	err := run(*o)
	if err == nil {
		t.Fatal("interactive mode without a terminal must fail, not hang")
	}
	for _, want := range []string{"--yes", "--help", "no terminal"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestHeadlessFlagsCoverAllStateKnobs(t *testing.T) {
	o := parseFlags([]string{
		"--yes", "--apps", "sonarr",
		"--downloads-root", "/mnt/nvme/dl",
		"--jellyfin-host-network",
	}, os.Stdout)
	if o.downloadsRoot != "/mnt/nvme/dl" || !o.jellyfinHostNet {
		t.Fatalf("parsed: %+v", o)
	}
}

func TestTrashFlags(t *testing.T) {
	o := parseFlags([]string{"--trash", "--trash-resolution", "2160p", "--trash-anime"}, os.Stdout)
	if !o.trash || o.trashResolution != "2160p" || o.trashSource != "bluray-web" || !o.trashAnime {
		t.Fatalf("parsed: %+v", o)
	}
}

func TestUpdateRefusesWithoutState(t *testing.T) {
	o := parseFlags([]string{"--state", "/nonexistent/dir/arrsenal.yaml"}, os.Stdout)
	err := runUpdate(*o)
	if err == nil || !strings.Contains(err.Error(), "nothing installed") {
		t.Fatalf("update without a state file must explain itself: %v", err)
	}
}
