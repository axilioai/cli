// Package session persists the CLI's "current session" — the phone lease that
// `axilio phone ...` verbs target by default, kubectl-context style. It is
// separate from credentials (internal/config): a session is ephemeral driving
// state, and it carries the control_url, which is minted only at allocate time
// (there is no endpoint to re-fetch it for an existing session), so the CLI must
// capture it at `sessions start` to drive later.
//
// Path: $XDG_CONFIG_HOME/axilio/session.json (else ~/.config/...), mode 0600 —
// the control_url embeds a scoped token.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Session is the on-disk current-session record.
type Session struct {
	SessionID  string `json:"session_id"`
	PhoneID    string `json:"phone_id"`
	PhoneType  string `json:"phone_type,omitempty"`
	ControlURL string `json:"control_url"`
}

// Path is the session file location, honouring XDG_CONFIG_HOME.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "axilio", "session.json")
}

// Load reads the current session. ok is false when none is set.
func Load() (Session, bool) {
	var s Session
	b, err := os.ReadFile(Path())
	if err != nil {
		return s, false
	}
	if err := json.Unmarshal(b, &s); err != nil || s.SessionID == "" {
		return Session{}, false
	}
	return s, true
}

// Save writes the current session, readable only by the owner (0600).
func Save(s Session) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// Clear removes the current-session file. Absent is not an error.
func Clear() error {
	if err := os.Remove(Path()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Matches reports whether id names this session (by session id or phone id).
func (s Session) Matches(id string) bool {
	return id != "" && (id == s.SessionID || id == s.PhoneID)
}
