package preflight

import (
	"strings"
	"testing"
)

const mountsFixture = `sysfs /sys sysfs rw,nosuid 0 0
proc /proc proc rw,nosuid 0 0
udev /dev devtmpfs rw,nosuid 0 0
tmpfs /run tmpfs rw,nosuid 0 0
/dev/sda2 / ext4 rw,relatime 0 0
/dev/sdb1 /mnt/das xfs rw,relatime 0 0
1TB-A:2TB-B /mnt/pool fuse.mergerfs rw,relatime 0 0
nas:/export /mnt/nas nfs4 rw,relatime 0 0
/dev/loop3 /snap/core/1234 squashfs ro 0 0
overlay /var/lib/docker/overlay2/x/merged overlay rw 0 0
/dev/sdc1 /mnt/my\040disk ext4 rw 0 0
cgroup2 /sys/fs/cgroup cgroup2 rw 0 0
`

func TestParseMountsFiltersPseudoFilesystems(t *testing.T) {
	got := parseMounts(strings.NewReader(mountsFixture))
	targets := map[string]string{}
	for _, m := range got {
		targets[m.Target] = m.FSType
	}
	for target, fstype := range map[string]string{
		"/":            "ext4",
		"/mnt/das":     "xfs",
		"/mnt/pool":    "fuse.mergerfs",
		"/mnt/nas":     "nfs4",
		"/mnt/my disk": "ext4", // octal-escaped space decoded
	} {
		if targets[target] != fstype {
			t.Errorf("%s: got %q, want %q (parsed: %v)", target, targets[target], fstype, targets)
		}
	}
	for _, gone := range []string{"/sys", "/proc", "/run", "/snap/core/1234", "/var/lib/docker/overlay2/x/merged", "/sys/fs/cgroup"} {
		if _, present := targets[gone]; present {
			t.Errorf("%s should have been filtered", gone)
		}
	}
}

func TestMountForLongestPrefixWins(t *testing.T) {
	mounts := parseMounts(strings.NewReader(mountsFixture))
	cases := map[string]string{
		"/mnt/das/data":   "/mnt/das",
		"/mnt/pool/media": "/mnt/pool",
		"/data":           "/",
		"/mnt/dasher":     "/", // prefix must respect path boundaries
	}
	for path, want := range cases {
		m, ok := MountFor(path, mounts)
		if !ok || m.Target != want {
			t.Errorf("MountFor(%s) = %q (%v), want %q", path, m.Target, ok, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[uint64]string{
		512:                            "512 B",
		2 * 1024 * 1024:                "2.0 MiB",
		54 * 1024 * 1024 * 1024 * 1024: "54.0 TiB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
