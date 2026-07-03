package preflight

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectTZFromDebianFile(t *testing.T) {
	dir := t.TempDir()
	tzFile := filepath.Join(dir, "timezone")
	if err := os.WriteFile(tzFile, []byte("America/Los_Angeles\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectTZAt(tzFile, filepath.Join(dir, "nope")); got != "America/Los_Angeles" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectTZFromLocaltimeSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "zoneinfo", "Europe", "Berlin")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "localtime")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if got := detectTZAt(filepath.Join(dir, "nope"), link); got != "Europe/Berlin" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectTZFallsBackToUTC(t *testing.T) {
	dir := t.TempDir()
	if got := detectTZAt(filepath.Join(dir, "a"), filepath.Join(dir, "b")); got != "Etc/UTC" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectIdentityWithoutSudoIsProcessOwner(t *testing.T) {
	t.Setenv("SUDO_UID", "")
	t.Setenv("SUDO_GID", "")
	uid, gid := DetectIdentity()
	if uid < 0 || gid < 0 {
		t.Fatalf("identity = %d:%d", uid, gid)
	}
}
