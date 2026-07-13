// Command axilio is the Axilio CLI: acquire and drive phones from the terminal.
package main

import (
	"context"
	"os"

	"github.com/axilioai/cli/cmd"
	"github.com/charmbracelet/fang"
)

func main() {
	// fang wraps cobra with styled help/errors, --version, and shell completions.
	if err := fang.Execute(context.Background(), cmd.Root()); err != nil {
		os.Exit(1)
	}
}
