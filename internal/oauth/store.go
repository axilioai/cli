// Package oauth implements the CLI's browser-OAuth login (AXI-1265): a PKCE
// authorization-code flow against the backend's authorization server, storing
// the resulting Axilio session tokens in the OS keychain (with a file fallback)
// and refreshing them proactively before each use.
package oauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "axilio-cli"
	keyringUser    = "oauth-tokens"
)

// Tokens is a stored OAuth session. Host records which API base the tokens
// authenticate against, so a session for one host is never reused against
// another (e.g. staging tokens against prod).
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Host         string    `json:"host"`
}

// filePath is the fallback token file, used when the OS keychain is
// unavailable (headless Linux, CI, SSH). Mode 0600.
func filePath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "axilio", "oauth.json")
}

// Save persists tokens to the OS keychain, falling back to a 0600 file when the
// keychain can't be written.
func Save(t Tokens) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if keyring.Set(keyringService, keyringUser, string(b)) == nil {
		return nil
	}
	p := filePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Load returns the stored tokens and whether a valid session was found, trying
// the keychain first and then the file fallback.
func Load() (Tokens, bool) {
	if s, err := keyring.Get(keyringService, keyringUser); err == nil {
		if t, ok := decode([]byte(s)); ok {
			return t, true
		}
	}
	b, err := os.ReadFile(filePath())
	if err != nil {
		return Tokens{}, false
	}
	return decode(b)
}

func decode(b []byte) (Tokens, bool) {
	var t Tokens
	if json.Unmarshal(b, &t) != nil || t.AccessToken == "" {
		return Tokens{}, false
	}
	return t, true
}

// Clear removes the stored session from both backends.
func Clear() {
	_ = keyring.Delete(keyringService, keyringUser)
	_ = os.Remove(filePath())
}

// HasSession reports whether an OAuth session is stored (any host).
func HasSession() bool {
	_, ok := Load()
	return ok
}
