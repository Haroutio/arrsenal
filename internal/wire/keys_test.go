package wire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Haroutio/arrsenal/internal/registry"
)

const arrConfigXML = `<Config>
  <BindAddress>*</BindAddress>
  <Port>8989</Port>
  <ApiKey>abcdef0123456789abcdef0123456789</ApiKey>
  <AuthenticationMethod>None</AuthenticationMethod>
</Config>`

const sabnzbdINI = `[misc]
queue_complete = ""
api_key = fedcba9876543210fedcba9876543210
nzb_key = 0000000000000000
[servers]
`

func app(t *testing.T, id string) registry.App {
	t.Helper()
	a, ok := registry.ByID(id)
	if !ok {
		t.Fatalf("registry lost %s", id)
	}
	return a
}

func TestReadKeyBrownfieldConfigsReadInstantly(t *testing.T) {
	root := t.TempDir()
	sonarr := app(t, "sonarr")
	if err := os.MkdirAll(filepath.Join(root, "sonarr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sonarr", "config.xml"), []byte(arrConfigXML), 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	key, err := ReadKey(context.Background(), sonarr, root, 5*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if key != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("key = %q", key)
	}
	if time.Since(start) > time.Second {
		t.Fatal("an existing config must be read on the first poll, not waited on")
	}
}

func TestReadKeySABnzbdINI(t *testing.T) {
	root := t.TempDir()
	sab := app(t, "sabnzbd")
	if err := os.MkdirAll(filepath.Join(root, "sabnzbd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sabnzbd", "sabnzbd.ini"), []byte(sabnzbdINI), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := ReadKey(context.Background(), sab, root, 5*time.Second, time.Second)
	if err != nil || key != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("key = %q, err = %v", key, err)
	}
}

func TestReadKeyPollsUntilTheAppWrites(t *testing.T) {
	root := t.TempDir()
	sonarr := app(t, "sonarr")
	dir := filepath.Join(root, "sonarr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The app "boots" concurrently: empty file first (mid-write), real
	// config shortly after.
	if err := os.WriteFile(filepath.Join(dir, "config.xml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "config.xml"), []byte(arrConfigXML), 0o644)
	}()

	key, err := ReadKey(context.Background(), sonarr, root, 5*time.Second, 10*time.Millisecond)
	if err != nil || key == "" {
		t.Fatalf("poll should survive the empty-then-written sequence: %q %v", key, err)
	}
}

func TestReadKeyTimeoutNamesTheContainer(t *testing.T) {
	root := t.TempDir()
	sonarr := app(t, "sonarr")
	_, err := ReadKey(context.Background(), sonarr, root, 50*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("no config must time out")
	}
	for _, want := range []string{"Sonarr", "docker logs sonarr"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("timeout error should contain %q: %v", want, err)
		}
	}
}

func TestReadKeyContextCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadKey(ctx, app(t, "sonarr"), root, time.Minute, 10*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestReadKeyRefusesKeylessApps(t *testing.T) {
	_, err := ReadKey(context.Background(), app(t, "homepage"), t.TempDir(), time.Second, time.Millisecond)
	if !errors.Is(err, ErrNoKeySource) {
		t.Fatalf("homepage has no key source, got %v", err)
	}
}

func TestRegistryKeySources(t *testing.T) {
	// The five key-bearing apps, and only they, declare sources.
	want := map[string]registry.KeyFormat{
		"prowlarr": registry.KeyXMLApiKey,
		"sonarr":   registry.KeyXMLApiKey,
		"radarr":   registry.KeyXMLApiKey,
		"lidarr":   registry.KeyXMLApiKey,
		"sabnzbd":  registry.KeyINIApiKey,
	}
	for _, a := range registry.All() {
		if wantFmt, ok := want[a.ID]; ok {
			if a.Key.Format != wantFmt || a.Key.File == "" {
				t.Errorf("%s: key source = %+v, want format %q", a.ID, a.Key, wantFmt)
			}
		} else if a.Key.Format != registry.KeyNone {
			t.Errorf("%s: unexpected key source %+v", a.ID, a.Key)
		}
	}
}
