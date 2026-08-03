#!/bin/sh
# Verify that the checked-in axilio(1) outputs are current and roff is lint-clean.
set -eu

command -v go >/dev/null 2>&1 || {
	printf 'error: go is required to verify the generated manual pages\n' >&2
	exit 1
}
command -v mandoc >/dev/null 2>&1 || {
	printf 'error: mandoc is required to verify man/axilio.1\n' >&2
	exit 1
}

go run ./cmd/manpage --check man/axilio.1
sh scripts/generate_manpage_html.sh --check man/axilio.1

lint_output=$(mktemp)
trap 'rm -f "$lint_output"' EXIT
if ! mandoc -Tlint man/axilio.1 >"$lint_output" 2>&1; then
	printf 'error: mandoc could not lint man/axilio.1\n' >&2
	cat "$lint_output" >&2
	exit 1
fi
if [ -s "$lint_output" ]; then
	printf 'error: mandoc reported diagnostics for man/axilio.1\n' >&2
	cat "$lint_output" >&2
	exit 1
fi
