// Command axilio is the Axilio CLI: acquire and drive phones from the terminal.
package main

import (
	"context"
	"os"

	"github.com/axilioai/cli/cmd"
	"github.com/axilioai/cli/internal/exit"
	"github.com/charmbracelet/fang"
)

func main() {
	// fang wraps cobra with styled help/errors, --version, and shell completions.
	// It renders the error to stderr; we map it onto a stable exit code (see
	// internal/exit) so agents branch on the status, not on stderr text.
	// WithoutVersion: the root command owns --version (cmd.Root sets root.Version
	// and a custom template) so the output carries the full commit + go version
	// + build date rather than fang's truncated form.
	if err := fang.Execute(
		context.Background(),
		cmd.Root(),
		fang.WithoutVersion(),
	); err != nil {
		os.Exit(int(exit.Classify(err)))
	}
}
