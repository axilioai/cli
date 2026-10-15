#!/bin/sh
# Generate the shell completion scripts that ship inside release archives.
# Like the HTML manual, these derive from the command tree at package time
# and are not checked in. The Homebrew cask installs them via its completion
# stanzas; Homebrew derives the installed file names from these basenames
# (axilio.bash -> axilio, _axilio stays _axilio, axilio.fish stays).
set -eu

command -v go >/dev/null 2>&1 || {
	printf 'error: go is required to generate completion scripts\n' >&2
	exit 1
}

outdir=${1:-completions}
mkdir -p "$outdir"
go run ./cmd/axilio completion bash >"$outdir/axilio.bash"
go run ./cmd/axilio completion zsh >"$outdir/_axilio"
go run ./cmd/axilio completion fish >"$outdir/axilio.fish"
