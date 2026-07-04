package dockerx

import (
	"strings"
	"testing"
)

// The canned output below is the real shape docker pull writes to a pipe
// (captured live) — status lines only, no byte counts.
const cannedPull = `3-alpine: Pulling from library/python
55afa1ecc21d: Pulling fs layer
86c5892c80ab: Pulling fs layer
f0017eadd506: Pulling fs layer
55afa1ecc21d: Download complete
86c5892c80ab: Download complete
55afa1ecc21d: Pull complete
f0017eadd506: Download complete
86c5892c80ab: Pull complete
f0017eadd506: Pull complete
Digest: sha256:2673086900deadbeef
Status: Downloaded newer image for python:3-alpine
`

func TestParsePullStreamCountsLayers(t *testing.T) {
	var last [2]int
	calls := 0
	parsePullStream(strings.NewReader(cannedPull), func(done, total int) {
		last = [2]int{done, total}
		calls++
		if done > total {
			t.Fatalf("done %d > total %d", done, total)
		}
	})
	if last != [2]int{3, 3} {
		t.Fatalf("final progress = %v, want 3/3", last)
	}
	if calls == 0 {
		t.Fatal("progress never reported")
	}
}

func TestParsePullStreamCachedImage(t *testing.T) {
	// Every layer already local: "Already exists" both discovers and
	// finishes each layer.
	canned := `latest: Pulling from linuxserver/sonarr
aa1: Already exists
bb2: Already exists
Digest: sha256:feedface
Status: Image is up to date for lscr.io/linuxserver/sonarr:latest
`
	var last [2]int
	parsePullStream(strings.NewReader(canned), func(done, total int) { last = [2]int{done, total} })
	if last != [2]int{2, 2} {
		t.Fatalf("cached image progress = %v, want 2/2", last)
	}
}

func TestParsePullStreamKeepsErrorTail(t *testing.T) {
	canned := "latest: Pulling from nope/nope\nError response from daemon: pull access denied"
	tail := parsePullStream(strings.NewReader(canned), nil)
	if len(tail) == 0 || !strings.Contains(strings.Join(tail, "\n"), "access denied") {
		t.Fatalf("error tail lost: %v", tail)
	}
}

func TestComposeImagesDedupes(t *testing.T) {
	d := NewWithRunner(nil, func(dir string, args ...string) (string, error) {
		if strings.Join(args, " ") != "compose config --images" {
			t.Fatalf("args: %v", args)
		}
		return "lscr.io/linuxserver/sonarr:latest\nlscr.io/linuxserver/sonarr:latest\nghcr.io/recyclarr/recyclarr:8\n", nil
	})
	images, err := d.ComposeImages("/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || images[0] != "lscr.io/linuxserver/sonarr:latest" {
		t.Fatalf("images: %v", images)
	}
}
