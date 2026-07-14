// Package session is the CLI's registry of active phone leases — the state the
// `axilio phone ...` verbs drive. It is built for parallelism: many terminals /
// agent processes can each hold their own lease and drive their own phone at
// once, with no shared-mutable state.
//
// Layout ($XDG_CONFIG_HOME/axilio, else ~/.config/axilio), mode 0600:
//
//	sessions/<session-id>.json   one file per active lease (atomic writes)
//	current-session              a convenience pointer (last-started id)
//
// Each lease carries its own control_url — minted only at allocate time (no
// endpoint re-fetches it), so it is captured at `sessions start` to drive later.
//
// Selection precedence (Resolve): --session flag > AXILIO_SESSION env > the sole
// active lease > the current-session pointer > error listing the active leases.
// AXILIO_SESSION is the parallelism primitive: each terminal pins its own phone.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EnvVar names the per-terminal session selector.
const EnvVar = "AXILIO_SESSION"

// Session is one active lease.
type Session struct {
	SessionID  string    `json:"session_id"`
	PhoneID    string    `json:"phone_id"`
	PhoneType  string    `json:"phone_type,omitempty"`
	ControlURL string    `json:"control_url"`
	CreatedAt  time.Time `json:"created_at"`
}

func baseDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "axilio")
}

// Dir is the per-lease registry directory.
func Dir() string { return filepath.Join(baseDir(), "sessions") }

func leasePath(id string) string { return filepath.Join(Dir(), id+".json") }

func currentPath() string { return filepath.Join(baseDir(), "current-session") }

// Matches reports whether id names this lease (by session id or phone id).
func (s Session) Matches(id string) bool {
	return id != "" && (id == s.SessionID || id == s.PhoneID)
}

// Save writes a lease atomically (temp + rename) so concurrent terminals never
// see a torn file, and records it as the current-session pointer.
func Save(s Session) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, leasePath(s.SessionID)); err != nil {
		return err
	}
	return setCurrent(s.SessionID)
}

// Get returns a lease by session id.
func Get(id string) (Session, bool) {
	b, err := os.ReadFile(leasePath(id))
	if err != nil {
		return Session{}, false
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil || s.SessionID == "" {
		return Session{}, false
	}
	return s, true
}

// List returns every active lease, oldest first.
func List() []Session {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return nil
	}
	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if s, ok := Get(id); ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// find returns the lease matching id (by session or phone id).
func find(id string) (Session, bool) {
	if id == "" {
		return Session{}, false
	}
	for _, s := range List() {
		if s.Matches(id) {
			return s, true
		}
	}
	return Session{}, false
}

// Remove deletes the lease named by id (session or phone id) and clears the
// current pointer if it pointed there. Absent is not an error.
func Remove(id string) error {
	s, ok := find(id)
	if !ok {
		return nil
	}
	if err := os.Remove(leasePath(s.SessionID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if readCurrent() == s.SessionID {
		_ = os.Remove(currentPath())
	}
	return nil
}

// Resolve applies the selection precedence and returns the lease to drive.
func Resolve(explicit string) (Session, error) {
	all := List()
	// 1. explicit --session
	if explicit != "" {
		if s, ok := find(explicit); ok {
			return s, nil
		}
		return Session{}, notActive(explicit, all)
	}
	// 2. AXILIO_SESSION env (the per-terminal primitive)
	if env := strings.TrimSpace(os.Getenv(EnvVar)); env != "" {
		if s, ok := find(env); ok {
			return s, nil
		}
		return Session{}, fmt.Errorf("%s=%q is not an active session%s", EnvVar, env, activeList(all))
	}
	// 3. sole active lease
	switch len(all) {
	case 0:
		return Session{}, fmt.Errorf("no active session; run `axilio sessions start`")
	case 1:
		return all[0], nil
	}
	// 4. current-session pointer (single-phone interactive convenience)
	if cur := readCurrent(); cur != "" {
		if s, ok := Get(cur); ok {
			return s, nil
		}
	}
	// 5. ambiguous
	return Session{}, fmt.Errorf(
		"%d active sessions; pass --session <id> or set %s%s", len(all), EnvVar, activeList(all))
}

func notActive(id string, all []Session) error {
	return fmt.Errorf("no active session %q%s", id, activeList(all))
}

func activeList(all []Session) string {
	if len(all) == 0 {
		return " (none active)"
	}
	ids := make([]string, len(all))
	for i, s := range all {
		ids[i] = s.SessionID
	}
	return " (active: " + strings.Join(ids, ", ") + ")"
}

func setCurrent(id string) error {
	if err := os.MkdirAll(baseDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(currentPath(), []byte(id+"\n"), 0o600)
}

func readCurrent() string {
	b, err := os.ReadFile(currentPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Current returns the current-session pointer id (may be empty).
func Current() string { return readCurrent() }
