#!/bin/sh
# Generate the self-contained HTML manual from the checked-in roff page. The
# output is a build artifact packaged into release archives, not a tracked
# file.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(dirname -- "$script_dir")
if [ "$#" -gt 1 ]; then
	printf 'usage: %s [man/axilio.1]\n' "$0" >&2
	exit 2
fi

manpage=${1:-"$repo_dir/man/axilio.1"}
case "$manpage" in
	/*) ;;
	*) manpage="$PWD/$manpage" ;;
esac

cd "$repo_dir"
exec go run ./cmd/manpage --html-only "$manpage"
