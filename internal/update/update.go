// Package update implements a passive "a newer version is available" nudge.
// It checks the CLI's GitHub releases at most once per interval (cached to a
// small file), never blocks meaningfully, and never surfaces an error to the
// caller: a failed check just prints nothing.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/semver"
)

const (
	releasesURL  = "https://api.github.com/repos/axilioai/cli/releases/latest"
	checkEvery   = 24 * time.Hour
	fetchTimeout = 1500 * time.Millisecond
)

// cacheState is the throttle/result cache persisted between runs.
type cacheState struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest"` // latest release tag seen ("" = none / unknown)
}

func cachePath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "axilio", "update-check.json")
}

// Notify prints an upgrade hint to w when current is a release version behind
// the latest GitHub release. Dev / source builds (non-semver versions) are
// never nagged. Safe to call on every command.
func Notify(ctx context.Context, w io.Writer, current string) {
	if !IsReleaseVersion(current) {
		return // dev / source build: don't nag
	}
	latest := latestVersion(ctx)
	if latest == "" {
		return
	}
	if Newer(latest, current) {
		fmt.Fprintf(w, "\nA new release of axilio is available: %s -> %s\nUpgrade with `axilio upgrade` or `go install github.com/axilioai/cli@latest`.\n",
			current, latest)
	}
}

// IsReleaseVersion reports whether v is a real released (semver) version rather
// than a dev / source / go-install build ("dev", a bare commit, etc.). Only
// release binaries can self-update; everything else defers to the toolchain.
func IsReleaseVersion(v string) bool {
	return semver.IsValid(ensureV(v))
}

// Newer reports whether release version latest is strictly newer than current.
// Both may be bare ("0.2.0") or v-prefixed; invalid inputs compare as not newer.
func Newer(latest, current string) bool {
	return semver.Compare(ensureV(latest), ensureV(current)) > 0
}

// latestVersion returns the latest known release tag, reading the cache when it
// is fresh and otherwise doing one bounded GitHub fetch (updating the cache).
func latestVersion(ctx context.Context) string {
	c, _ := readCache()
	if time.Since(c.LastCheck) < checkEvery {
		return c.Latest
	}
	latest, err := fetchLatest(ctx)
	if err != nil {
		return c.Latest // keep the last known value on failure
	}
	_ = writeCache(cacheState{LastCheck: time.Now(), Latest: latest})
	return latest
}

// fetchLatest returns just the latest release tag for the passive notifier,
// delegating to the shared release fetch. A missing release ("" tag) is the
// no-releases-yet signal the cache stores.
func fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	rel, err := FetchLatestRelease(ctx)
	if err != nil || rel == nil {
		return "", err
	}
	return rel.Tag, nil
}

func readCache() (cacheState, error) {
	var c cacheState
	b, err := os.ReadFile(cachePath())
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}

func writeCache(c cacheState) error {
	p := cachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// ensureV prefixes a bare semver ("0.1.0") with "v" so golang.org/x/mod/semver
// accepts it; leaves an already-prefixed or invalid value unchanged.
func ensureV(v string) string {
	if v == "" || v[0] == 'v' {
		return v
	}
	return "v" + v
}
