package cmd

import (
	"context"

	"github.com/axilioai/cli/internal/update"
	"github.com/spf13/cobra"
)

func upgradeCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Update axilio to the latest release.",
		Long: "Download the latest release from GitHub and replace this binary in place " +
			"(the download is checksum-verified). Homebrew installs defer to `brew upgrade`, " +
			"and development / `go install` builds are left to the Go toolchain. Pass " +
			"`--check` to see whether a newer release exists without installing it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpgrade(cmd.Context(), check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only report whether a newer release is available; do not install")
	return cmd
}

func runUpgrade(ctx context.Context, check bool) error {
	p := printer()

	// Homebrew owns its binary; replacing it out from under brew breaks
	// `brew upgrade`/`uninstall` bookkeeping.
	if update.IsHomebrew() {
		p.Note("This axilio was installed with Homebrew. Upgrade with:\n  brew upgrade axilio")
		return nil
	}
	// A dev / source / `go install` build has no release binary to swap in; the
	// toolchain manages it.
	if !update.IsReleaseVersion(Version) {
		p.Note("This is a development build (%s). Install a release with:\n  go install github.com/axilioai/cli@latest", versionString())
		return nil
	}

	rel, err := update.FetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	if rel == nil || rel.Tag == "" {
		p.Note("No releases have been published yet.")
		return nil
	}
	if !update.Newer(rel.Tag, Version) {
		p.Note("axilio is up to date (%s).", Version)
		return nil
	}
	if check {
		p.Note("A newer release is available: %s -> %s. Run `axilio upgrade` to install.", Version, rel.Tag)
		return nil
	}

	p.Note("Upgrading axilio %s -> %s...", Version, rel.Tag)
	if err := update.Apply(ctx, rel); err != nil {
		return err
	}
	p.Note("Upgraded to %s.", rel.Tag)
	return nil
}
