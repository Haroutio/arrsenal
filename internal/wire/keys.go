package wire

import (
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Haroutio/arrsenal/internal/registry"
)

// ErrNoKeySource marks apps whose registry entry declares no readable key.
var ErrNoKeySource = errors.New("app has no readable key source")

// ReadKey polls an app's config until the key it generated on first boot
// materializes (DESIGN.md §7.2). The same read serves brownfield adoption:
// an existing config's key is returned on the first poll, untouched. The
// timeout error names the container and the one command to investigate.
func ReadKey(ctx context.Context, app registry.App, appdataRoot string, timeout, poll time.Duration) (string, error) {
	if app.Key.Format == registry.KeyNone {
		return "", fmt.Errorf("%w: %s", ErrNoKeySource, app.ID)
	}
	path := filepath.Join(appdataRoot, app.ID, app.Key.File)
	deadline := time.Now().Add(timeout)

	for {
		key, err := parseKeyFile(path, app.Key.Format)
		if err == nil && key != "" {
			return key, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf(
				"%s never wrote its API key to %s within %s — is the container healthy? check: docker logs %s",
				app.Name, path, timeout, app.ID)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
}

// parseKeyFile extracts a key from one supported format. Unreadable or
// not-yet-complete files are simply "not yet" — the poll loop's problem.
func parseKeyFile(path string, format registry.KeyFormat) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	switch format {
	case registry.KeyXMLApiKey:
		// The arr family: <Config>...<ApiKey>hex</ApiKey>...</Config>
		var cfg struct {
			APIKey string `xml:"ApiKey"`
		}
		if err := xml.NewDecoder(f).Decode(&cfg); err != nil {
			return "", err // partial write mid-boot: retry
		}
		return strings.TrimSpace(cfg.APIKey), nil

	case registry.KeyINIApiKey:
		// SABnzbd: `api_key = hex` in sabnzbd.ini. A plain scan beats an
		// INI dependency for one stable key in one stable file.
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, found := strings.Cut(sc.Text(), "=")
			if found && strings.TrimSpace(k) == "api_key" {
				return strings.TrimSpace(v), nil
			}
		}
		return "", sc.Err()

	default:
		return "", fmt.Errorf("unsupported key format %q", format)
	}
}
