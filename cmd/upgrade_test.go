package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/axilioai/cli/internal/update"
)

func captureUpgradeJSON(t *testing.T, check bool, deps upgradeDependencies) (upgradeResult, error) {
	t.Helper()
	originalOutput := flagOutput
	originalQuiet := flagQuiet
	originalStdout := os.Stdout
	flagOutput = "json"
	flagQuiet = false
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		flagOutput = originalOutput
		flagQuiet = originalQuiet
		os.Stdout = originalStdout
	})

	runErr := runUpgrade(context.Background(), check, deps)
	_ = w.Close()
	os.Stdout = originalStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	if runErr != nil {
		return upgradeResult{}, runErr
	}
	var result upgradeResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("upgrade result is not JSON: %v\n%s", err, buf.String())
	}
	return result, nil
}

func TestUpgradeDecisionMatrix(t *testing.T) {
	release := &update.Release{Tag: "v0.7.0"}
	fetchFailure := errors.New("release lookup failed")
	tests := []struct {
		name        string
		version     string
		check       bool
		homebrew    bool
		release     *update.Release
		fetchErr    error
		want        upgradeResult
		wantFetches int
		wantApplies int
	}{
		{
			name:    "development build stays toolchain-managed",
			version: "dev",
			check:   true,
			want: upgradeResult{
				Status: "dev-build", Current: "dev", InstallMethod: "go-toolchain",
				NextCommand: "go install github.com/axilioai/cli@latest",
			},
		},
		{
			name:     "Homebrew apply returns guidance without fetching",
			version:  "v0.6.0",
			homebrew: true,
			release:  release,
			want: upgradeResult{
				Status: "homebrew-managed", Current: "v0.6.0", InstallMethod: "homebrew",
				NextCommand: "brew upgrade axilio",
			},
		},
		{
			name:        "Homebrew check fetches without applying",
			version:     "v0.6.0",
			check:       true,
			homebrew:    true,
			release:     release,
			wantFetches: 1,
			want: upgradeResult{
				Status: "update-available", Current: "v0.6.0", Latest: "v0.7.0",
				UpdateAvailable: true, InstallMethod: "homebrew", NextCommand: "brew upgrade axilio",
			},
		},
		{
			name:        "standalone check never applies",
			version:     "v0.6.0",
			check:       true,
			release:     release,
			wantFetches: 1,
			want: upgradeResult{
				Status: "update-available", Current: "v0.6.0", Latest: "v0.7.0",
				UpdateAvailable: true, InstallMethod: "standalone", NextCommand: "axilio upgrade",
			},
		},
		{
			name:        "standalone apply installs available release",
			version:     "v0.6.0",
			release:     release,
			wantFetches: 1,
			wantApplies: 1,
			want: upgradeResult{
				Status: "upgraded", Current: "v0.6.0", Latest: "v0.7.0", InstallMethod: "standalone",
			},
		},
		{
			name:        "empty release feed is successful",
			version:     "v0.6.0",
			check:       true,
			homebrew:    true,
			wantFetches: 1,
			want: upgradeResult{
				Status: "no-releases", Current: "v0.6.0", InstallMethod: "homebrew",
			},
		},
		{
			name:        "fetch failure is returned without applying",
			version:     "v0.6.0",
			check:       true,
			homebrew:    true,
			fetchErr:    fetchFailure,
			wantFetches: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalVersion := Version
			Version = tc.version
			t.Cleanup(func() { Version = originalVersion })

			fetches, applies := 0, 0
			result, err := captureUpgradeJSON(t, tc.check, upgradeDependencies{
				isHomebrew: func() bool { return tc.homebrew },
				fetchLatest: func(context.Context) (*update.Release, error) {
					fetches++
					return tc.release, tc.fetchErr
				},
				apply: func(_ context.Context, got *update.Release) error {
					applies++
					if tc.release == nil || got == nil || got.Tag != tc.release.Tag {
						t.Errorf("applied release = %#v, want %#v", got, tc.release)
					}
					return nil
				},
			})
			switch {
			case tc.fetchErr != nil:
				if !errors.Is(err, tc.fetchErr) {
					t.Fatalf("error = %v, want %v", err, tc.fetchErr)
				}
			case err != nil:
				t.Fatal(err)
			case result != tc.want:
				t.Fatalf("result = %+v, want %+v", result, tc.want)
			}
			if fetches != tc.wantFetches || applies != tc.wantApplies {
				t.Fatalf("fetches/applies = %d/%d, want %d/%d", fetches, applies, tc.wantFetches, tc.wantApplies)
			}
		})
	}
}
