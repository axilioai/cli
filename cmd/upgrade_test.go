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

	runErr := runUpgradeWithDependencies(context.Background(), check, deps)
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

func useReleaseVersion(t *testing.T) {
	t.Helper()
	original := Version
	Version = "v0.6.0"
	t.Cleanup(func() { Version = original })
}

func TestUpgradeDevBuildNoNetwork(t *testing.T) {
	fetches := 0
	applies := 0
	result, err := captureUpgradeJSON(t, true, upgradeDependencies{
		isHomebrew: func() bool { return true },
		fetchLatest: func(context.Context) (*update.Release, error) {
			fetches++
			return nil, nil
		},
		apply: func(context.Context, *update.Release) error {
			applies++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade --check on a dev build: %v", err)
	}
	if result.Status != "dev-build" || result.InstallMethod != "go-toolchain" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fetches != 0 || applies != 0 {
		t.Fatalf("dev build fetched %d times and applied %d times", fetches, applies)
	}
}

func TestUpgradeHomebrewApplyReturnsGuidanceWithoutFetch(t *testing.T) {
	useReleaseVersion(t)
	fetches := 0
	applies := 0
	result, err := captureUpgradeJSON(t, false, upgradeDependencies{
		isHomebrew: func() bool { return true },
		fetchLatest: func(context.Context) (*update.Release, error) {
			fetches++
			return &update.Release{Tag: "v0.7.0"}, nil
		},
		apply: func(context.Context, *update.Release) error {
			applies++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade on Homebrew install: %v", err)
	}
	if result.Status != "homebrew-managed" || result.InstallMethod != "homebrew" || result.NextCommand != "brew upgrade axilio" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fetches != 0 || applies != 0 {
		t.Fatalf("Homebrew apply path fetched %d times and applied %d times", fetches, applies)
	}
}

func TestUpgradeCheckHomebrewFetchesWithoutApplying(t *testing.T) {
	useReleaseVersion(t)
	fetches := 0
	applies := 0
	result, err := captureUpgradeJSON(t, true, upgradeDependencies{
		isHomebrew: func() bool { return true },
		fetchLatest: func(context.Context) (*update.Release, error) {
			fetches++
			return &update.Release{Tag: "v0.7.0"}, nil
		},
		apply: func(context.Context, *update.Release) error {
			applies++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade --check on Homebrew install: %v", err)
	}
	if result.Status != "update-available" || !result.UpdateAvailable || result.InstallMethod != "homebrew" || result.NextCommand != "brew upgrade axilio" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fetches != 1 || applies != 0 {
		t.Fatalf("Homebrew check fetched %d times and applied %d times", fetches, applies)
	}
}

func TestUpgradeCheckStandaloneNeverApplies(t *testing.T) {
	useReleaseVersion(t)
	applies := 0
	result, err := captureUpgradeJSON(t, true, upgradeDependencies{
		isHomebrew: func() bool { return false },
		fetchLatest: func(context.Context) (*update.Release, error) {
			return &update.Release{Tag: "v0.7.0"}, nil
		},
		apply: func(context.Context, *update.Release) error {
			applies++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade --check: %v", err)
	}
	if result.InstallMethod != "standalone" || result.NextCommand != "axilio upgrade" || !result.UpdateAvailable {
		t.Fatalf("unexpected result: %+v", result)
	}
	if applies != 0 {
		t.Fatalf("check path applied %d times", applies)
	}
}

func TestUpgradeStandaloneAppliesAvailableRelease(t *testing.T) {
	useReleaseVersion(t)
	applies := 0
	result, err := captureUpgradeJSON(t, false, upgradeDependencies{
		isHomebrew: func() bool { return false },
		fetchLatest: func(context.Context) (*update.Release, error) {
			return &update.Release{Tag: "v0.7.0"}, nil
		},
		apply: func(_ context.Context, rel *update.Release) error {
			applies++
			if rel.Tag != "v0.7.0" {
				t.Fatalf("apply release = %q", rel.Tag)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if result.Status != "upgraded" || result.Latest != "v0.7.0" || result.InstallMethod != "standalone" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if applies != 1 {
		t.Fatalf("apply calls = %d, want 1", applies)
	}
}

func TestUpgradeCheckNoReleases(t *testing.T) {
	useReleaseVersion(t)
	result, err := captureUpgradeJSON(t, true, upgradeDependencies{
		isHomebrew:  func() bool { return true },
		fetchLatest: func(context.Context) (*update.Release, error) { return nil, nil },
		apply: func(context.Context, *update.Release) error {
			t.Fatal("check must not apply")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade --check: %v", err)
	}
	if result.Status != "no-releases" || result.Latest != "" || result.UpdateAvailable || result.InstallMethod != "homebrew" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestUpgradeCheckSurfacesFetchFailure(t *testing.T) {
	useReleaseVersion(t)
	want := errors.New("release lookup failed")
	applies := 0
	_, err := captureUpgradeJSON(t, true, upgradeDependencies{
		isHomebrew:  func() bool { return true },
		fetchLatest: func(context.Context) (*update.Release, error) { return nil, want },
		apply: func(context.Context, *update.Release) error {
			applies++
			return nil
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if applies != 0 {
		t.Fatalf("failed check applied %d times", applies)
	}
}
