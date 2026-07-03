package wire

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"math/big"
)

// qBittorrent is the single pre-seed exception (DESIGN.md §7): its own
// generated WebUI password lands only in container logs, so Arrsenal
// generates one, persists it in the state's Secrets, and writes it into
// qBittorrent.conf BEFORE first start.

// qbitIterations matches qBittorrent's own PBKDF2 cost.
const qbitIterations = 100000

// GeneratePassword returns a URL-safe random password for the pre-seed.
func GeneratePassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, 24)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

// QBitConfig renders a minimal qBittorrent.conf pre-seeding the WebUI
// password (qBittorrent's own scheme: PBKDF2-HMAC-SHA512, 100k iterations,
// base64(salt):base64(dk) inside @ByteArray) and pointing the save paths at
// the shared data tree. Written only when the config is absent — an adopted
// qBittorrent keeps its own password (and its registration into the arrs
// then honestly fails with a fallback URL, since that password is unknown).
func QBitConfig(password string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	dk, err := pbkdf2.Key(sha512.New, password, salt, qbitIterations, 64)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("@ByteArray(%s:%s)",
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(dk))

	conf := fmt.Sprintf(`[BitTorrent]
Session\DefaultSavePath=/data/torrents
Session\TempPath=/data/torrents/incomplete
Session\TempPathEnabled=true

[Preferences]
WebUI\Username=admin
WebUI\Password_PBKDF2="%s"
`, hash)
	return []byte(conf), nil
}
