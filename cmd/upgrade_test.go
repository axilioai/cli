package cmd

import (
	"testing"

	"github.com/axilioai/cli/internal/update"
)

func TestUpgradeDecisionMatrix(t *testing.T) {
	release := &update.Release{Tag: "v0.7.0"}
	tests := []struct {
		name  string
		state upgradeState
		want  upgradeDecision
	}{
		{
			name:  "development build stays toolchain-managed",
			state: upgradeState{current: "dev", check: true},
			want: upgradeDecision{result: upgradeResult{
				Status: "dev-build", Current: "dev", InstallMethod: "go-toolchain",
				NextCommand: "go install github.com/axilioai/cli@latest",
			}},
		},
		{
			name:  "Homebrew apply returns guidance without fetching",
			state: upgradeState{current: "v0.6.0", homebrew: true},
			want: upgradeDecision{result: upgradeResult{
				Status: "homebrew-managed", Current: "v0.6.0", InstallMethod: "homebrew",
				NextCommand: "brew upgrade axilio",
			}},
		},
		{
			name:  "Homebrew check fetches release",
			state: upgradeState{current: "v0.6.0", check: true, homebrew: true},
			want:  upgradeDecision{action: upgradeFetch},
		},
		{
			name: "Homebrew check reports available release",
			state: upgradeState{
				current: "v0.6.0", check: true, homebrew: true,
				releaseChecked: true, release: release,
			},
			want: upgradeDecision{result: upgradeResult{
				Status: "update-available", Current: "v0.6.0", Latest: "v0.7.0",
				UpdateAvailable: true, InstallMethod: "homebrew", NextCommand: "brew upgrade axilio",
			}},
		},
		{
			name:  "standalone check fetches release",
			state: upgradeState{current: "v0.6.0", check: true},
			want:  upgradeDecision{action: upgradeFetch},
		},
		{
			name: "standalone check reports available release",
			state: upgradeState{
				current: "v0.6.0", check: true, releaseChecked: true, release: release,
			},
			want: upgradeDecision{result: upgradeResult{
				Status: "update-available", Current: "v0.6.0", Latest: "v0.7.0",
				UpdateAvailable: true, InstallMethod: "standalone", NextCommand: "axilio upgrade",
			}},
		},
		{
			name: "standalone apply installs available release",
			state: upgradeState{
				current: "v0.6.0", releaseChecked: true, release: release,
			},
			want: upgradeDecision{action: upgradeApply, result: upgradeResult{
				Status: "upgraded", Current: "v0.6.0", Latest: "v0.7.0", InstallMethod: "standalone",
			}},
		},
		{
			name: "empty release feed is successful",
			state: upgradeState{
				current: "v0.6.0", check: true, homebrew: true, releaseChecked: true,
			},
			want: upgradeDecision{result: upgradeResult{
				Status: "no-releases", Current: "v0.6.0", InstallMethod: "homebrew",
			}},
		},
		{
			name: "current release is up to date",
			state: upgradeState{
				current: "v0.7.0", check: true, releaseChecked: true, release: release,
			},
			want: upgradeDecision{result: upgradeResult{
				Status: "up-to-date", Current: "v0.7.0", Latest: "v0.7.0", InstallMethod: "standalone",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideUpgrade(tc.state); got != tc.want {
				t.Fatalf("decision = %+v, want %+v", got, tc.want)
			}
		})
	}
}
