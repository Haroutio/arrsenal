package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardlinkSucceedsOnSameFilesystem(t *testing.T) {
	linuxOnly(t)
	root := t.TempDir()
	dl := filepath.Join(root, "usenet")
	media := filepath.Join(root, "media")
	for _, d := range []string{dl, media} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	res := CheckHardlink(dl, media)
	if !res.OK {
		t.Fatalf("same-filesystem link failed: %s", res.Detail)
	}
	// No probe debris in either directory.
	for _, d := range []string{dl, media} {
		entries, _ := os.ReadDir(d)
		if len(entries) != 0 {
			t.Fatalf("probe left debris in %s: %v", d, entries)
		}
	}
}

func TestHardlinkFailsAcrossFilesystems(t *testing.T) {
	linuxOnly(t)
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("/dev/shm not available")
	}
	// /tmp (disk) and /dev/shm (tmpfs) are distinct filesystems — even when
	// /tmp is itself tmpfs, separate mounts have separate device IDs.
	dl := t.TempDir()
	media, err := os.MkdirTemp("/dev/shm", "arrsenal-test-*")
	if err != nil {
		t.Skip("cannot create test dir on /dev/shm")
	}
	t.Cleanup(func() { _ = os.RemoveAll(media) })

	res := CheckHardlink(dl, media)
	if res.OK {
		t.Fatal("cross-filesystem link reported OK")
	}
	for _, want := range []string{"COPY", "different filesystems"} {
		if !strings.Contains(res.Detail, want) {
			t.Errorf("detail should mention %q, got: %s", want, res.Detail)
		}
	}
}

func TestHardlinkRequiresExistingDirs(t *testing.T) {
	res := CheckHardlink(filepath.Join(t.TempDir(), "nope"), t.TempDir())
	if res.OK || !strings.Contains(res.Detail, "does not exist") {
		t.Fatalf("missing dir must fail loudly: %+v", res)
	}
}

func TestCheckOwnership(t *testing.T) {
	linuxOnly(t)
	dir := t.TempDir()
	c := CheckOwnership(dir, os.Getuid(), os.Getgid())
	if !c.Writable || !c.OwnerOK {
		t.Fatalf("own tempdir should be writable and owned: %+v", c)
	}

	c = CheckOwnership(dir, os.Getuid()+12345, os.Getgid())
	if c.OwnerOK {
		t.Fatal("wrong expected owner must be reported")
	}
	if !strings.Contains(c.Detail, "chown -R") {
		t.Fatalf("detail should include the fix command: %s", c.Detail)
	}

	missing := CheckOwnership(filepath.Join(dir, "nope"), os.Getuid(), os.Getgid())
	if missing.Writable || !strings.Contains(missing.Detail, "will be created") {
		t.Fatalf("missing path is informational, not an error: %+v", missing)
	}
}
