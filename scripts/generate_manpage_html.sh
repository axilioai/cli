#!/bin/sh
# Convert the checked-in roff manual to its self-contained HTML counterpart.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(dirname -- "$script_dir")
check=
if [ "${1-}" = "--check" ]; then
	check=--check
	shift
fi
if [ "$#" -gt 1 ]; then
	printf 'usage: %s [--check] [man/axilio.1]\n' "$0" >&2
	exit 2
fi

manpage=${1:-"$repo_dir/man/axilio.1"}
case "$manpage" in
	/*) ;;
	*) manpage="$PWD/$manpage" ;;
esac

cd "$repo_dir"
if [ -n "$check" ]; then
	exec go run ./cmd/manpage --html-only --check "$manpage"
fi
exec go run ./cmd/manpage --html-only "$manpage"
