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
	"net/http"
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
	cv := ensureV(current)
	if !semver.IsValid(cv) {
		return // dev / source build: don't nag
	}
	latest := latestVersion(ctx)
	if latest == "" {
		return
	}
	if semver.Compare(ensureV(latest), cv) > 0 {
		fmt.Fprintf(w, "\nA new release of axilio is available: %s -> %s\nUpgrade with `axilio upgrade` or `go install github.com/axilioai/cli@latest`.\n",
			current, latest)
	}
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

func fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// 404 = no releases yet; treat as "no update" and cache it so we don't
		// re-hit GitHub every command until the interval passes.
		return "", nil
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.TagName, nil
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
