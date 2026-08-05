package cmd

import (
	"context"

	"github.com/axilioai/cli/internal/update"
	"github.com/spf13/cobra"
)

type upgradeResult struct {
	Status          string `json:"status"`
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	InstallMethod   string `json:"install_method"`
	NextCommand     string `json:"next_command"`
}

type upgradeAction uint8

const (
	upgradeReport upgradeAction = iota
	upgradeFetch
	upgradeApply
)

type upgradeState struct {
	current        string
	check          bool
	homebrew       bool
	releaseChecked bool
	release        *update.Release
}

type upgradeDecision struct {
	action upgradeAction
	result upgradeResult
}

func upgradeCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Update axilio to the latest release.",
		Long: "Download the latest release from GitHub and replace this binary in place " +
			"(the download is checksum-verified). Homebrew installs defer to `brew upgrade`, " +
			"and development or `go install` builds are left to the Go toolchain. On a " +
			"release build, --check reports whether a newer release exists without " +
			"installing it, including for Homebrew-managed installations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpgrade(cmd.Context(), check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Check for a newer release without installing it")
	return cmd
}

func runUpgrade(ctx context.Context, check bool) error {
	p := printer()
	state := upgradeState{
		current:  Version,
		check:    check,
		homebrew: update.IsHomebrew(),
	}
	decision := decideUpgrade(state)
	if decision.action == upgradeFetch {
		rel, err := update.FetchLatestRelease(ctx)
		if err != nil {
			return err
		}
		state.releaseChecked = true
		state.release = rel
		decision = decideUpgrade(state)
	}
	if decision.action == upgradeApply {
		p.Note("Upgrading axilio %s -> %s...", Version, state.release.Tag)
		if err := update.Apply(ctx, state.release); err != nil {
			return err
		}
	}

	p.Emit(decision.result, func() {
		switch decision.result.Status {
		case "dev-build":
			p.Note("This is a development build (%s). Install a release with:\n  go install github.com/axilioai/cli@latest", versionString())
		case "homebrew-managed":
			p.Note("This axilio was installed with Homebrew. Upgrade with:\n  brew upgrade axilio")
		case "no-releases":
			p.Note("No releases have been published yet.")
		case "up-to-date":
			p.Note("axilio is up to date (%s).", Version)
		case "update-available":
			p.Note("A newer release is available: %s -> %s. Run `%s` to install.", Version, decision.result.Latest, decision.result.NextCommand)
		case "upgraded":
			p.Note("Upgraded to %s.", decision.result.Latest)
		}
	})
	return nil
}

func decideUpgrade(state upgradeState) upgradeDecision {
	if !update.IsReleaseVersion(state.current) {
		return upgradeDecision{result: upgradeResult{
			Status:        "dev-build",
			Current:       state.current,
			InstallMethod: "go-toolchain",
			NextCommand:   "go install github.com/axilioai/cli@latest",
		}}
	}
	if state.homebrew && !state.check {
		return upgradeDecision{result: upgradeResult{
			Status:        "homebrew-managed",
			Current:       state.current,
			InstallMethod: "homebrew",
			NextCommand:   "brew upgrade axilio",
		}}
	}
	if !state.releaseChecked {
		return upgradeDecision{action: upgradeFetch}
	}

	installMethod := "standalone"
	if state.homebrew {
		installMethod = "homebrew"
	}
	if state.release == nil || state.release.Tag == "" {
		return upgradeDecision{result: upgradeResult{
			Status:        "no-releases",
			Current:       state.current,
			InstallMethod: installMethod,
		}}
	}
	if !update.Newer(state.release.Tag, state.current) {
		return upgradeDecision{result: upgradeResult{
			Status:        "up-to-date",
			Current:       state.current,
			Latest:        state.release.Tag,
			InstallMethod: installMethod,
		}}
	}
	if state.check {
		nextCommand := "axilio upgrade"
		if state.homebrew {
			nextCommand = "brew upgrade axilio"
		}
		return upgradeDecision{result: upgradeResult{
			Status:          "update-available",
			Current:         state.current,
			Latest:          state.release.Tag,
			UpdateAvailable: true,
			InstallMethod:   installMethod,
			NextCommand:     nextCommand,
		}}
	}
	return upgradeDecision{
		action: upgradeApply,
		result: upgradeResult{
			Status:        "upgraded",
			Current:       state.current,
			Latest:        state.release.Tag,
			InstallMethod: installMethod,
		},
	}
}
