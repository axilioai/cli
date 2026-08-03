package cmd

import (
	"context"

	"github.com/axilioai/cli/internal/update"
	"github.com/spf13/cobra"
)

type upgradeDependencies struct {
	isHomebrew  func() bool
	fetchLatest func(context.Context) (*update.Release, error)
	apply       func(context.Context, *update.Release) error
}

type upgradeResult struct {
	Status          string `json:"status"`
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	InstallMethod   string `json:"install_method"`
	NextCommand     string `json:"next_command"`
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
	return runUpgradeWithDependencies(ctx, check, upgradeDependencies{
		isHomebrew:  update.IsHomebrew,
		fetchLatest: update.FetchLatestRelease,
		apply:       update.Apply,
	})
}

func runUpgradeWithDependencies(ctx context.Context, check bool, deps upgradeDependencies) error {
	p := printer()

	// A dev / source / `go install` build has no release binary to swap in; the
	// toolchain manages it.
	if !update.IsReleaseVersion(Version) {
		p.Emit(upgradeResult{
			Status:        "dev-build",
			Current:       Version,
			InstallMethod: "go-toolchain",
			NextCommand:   "go install github.com/axilioai/cli@latest",
		}, func() {
			p.Note("This is a development build (%s). Install a release with:\n  go install github.com/axilioai/cli@latest", versionString())
		})
		return nil
	}

	homebrew := deps.isHomebrew()
	// Homebrew owns its binary; replacing it out from under brew breaks
	// `brew upgrade`/`uninstall` bookkeeping. A non-check invocation therefore
	// returns guidance without fetching a release or touching the binary.
	if homebrew && !check {
		p.Emit(upgradeResult{
			Status:        "homebrew-managed",
			Current:       Version,
			InstallMethod: "homebrew",
			NextCommand:   "brew upgrade axilio",
		}, func() {
			p.Note("This axilio was installed with Homebrew. Upgrade with:\n  brew upgrade axilio")
		})
		return nil
	}

	rel, err := deps.fetchLatest(ctx)
	if err != nil {
		return err
	}
	installMethod := "standalone"
	if homebrew {
		installMethod = "homebrew"
	}
	if rel == nil || rel.Tag == "" {
		p.Emit(upgradeResult{
			Status:        "no-releases",
			Current:       Version,
			InstallMethod: installMethod,
		}, func() {
			p.Note("No releases have been published yet.")
		})
		return nil
	}
	if !update.Newer(rel.Tag, Version) {
		p.Emit(upgradeResult{
			Status:        "up-to-date",
			Current:       Version,
			Latest:        rel.Tag,
			InstallMethod: installMethod,
		}, func() {
			p.Note("axilio is up to date (%s).", Version)
		})
		return nil
	}
	if check {
		nextCommand := "axilio upgrade"
		if homebrew {
			nextCommand = "brew upgrade axilio"
		}
		p.Emit(upgradeResult{
			Status:          "update-available",
			Current:         Version,
			Latest:          rel.Tag,
			UpdateAvailable: true,
			InstallMethod:   installMethod,
			NextCommand:     nextCommand,
		}, func() {
			p.Note("A newer release is available: %s -> %s. Run `%s` to install.", Version, rel.Tag, nextCommand)
		})
		return nil
	}

	p.Note("Upgrading axilio %s -> %s...", Version, rel.Tag)
	if err := deps.apply(ctx, rel); err != nil {
		return err
	}
	p.Emit(upgradeResult{
		Status:        "upgraded",
		Current:       Version,
		Latest:        rel.Tag,
		InstallMethod: installMethod,
	}, func() {
		p.Note("Upgraded to %s.", rel.Tag)
	})
	return nil
}
