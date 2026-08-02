#!/bin/sh
# Verify the output of `goreleaser release --snapshot --clean`.
set -eu

dist=${1:-dist}
[ -d "$dist" ] || {
	printf 'FAIL: release directory does not exist: %s\n' "$dist" >&2
	exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
archives="$tmp/archives"
find "$dist" -maxdepth 1 -type f \( -name 'axilio_*.tar.gz' -o -name 'axilio_*.zip' \) | sort >"$archives"
archive_count=$(wc -l <"$archives" | tr -d '[:space:]')
[ "$archive_count" = 6 ] || {
	printf 'FAIL: found %s release archives, want 6\n' "$archive_count" >&2
	cat "$archives" >&2
	exit 1
}

checksums="$dist/checksums.txt"
[ -f "$checksums" ] || {
	printf 'FAIL: release checksums are missing: %s\n' "$checksums" >&2
	exit 1
}
checksum_count=$(wc -l <"$checksums" | tr -d '[:space:]')
[ "$checksum_count" = 6 ] || {
	printf 'FAIL: found %s release checksums, want 6\n' "$checksum_count" >&2
	exit 1
}
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$dist" && sha256sum -c checksums.txt >/dev/null)
else
	(cd "$dist" && shasum -a 256 -c checksums.txt >/dev/null)
fi

while IFS= read -r archive; do
	inventory="$tmp/inventory"
	expected="$tmp/expected"
	case "$archive" in
	*.tar.gz)
		tar -tzf "$archive" >"$inventory.raw"
		tar -xOzf "$archive" man/axilio.1 >"$tmp/archived-manpage"
		binary=axilio
		;;
	*.zip)
		unzip -Z1 "$archive" >"$inventory.raw"
		unzip -p "$archive" man/axilio.1 >"$tmp/archived-manpage"
		binary=axilio.exe
		;;
	esac
	sed -e '/\/$/d' -e 's#^\./##' "$inventory.raw" | LC_ALL=C sort >"$inventory"
	printf '%s\n' README.md "$binary" man/axilio.1 | LC_ALL=C sort >"$expected"
	if ! diff -u "$expected" "$inventory"; then
		printf 'FAIL: unexpected archive inventory: %s\n' "$archive" >&2
		exit 1
	fi
	if ! cmp man/axilio.1 "$tmp/archived-manpage"; then
		printf 'FAIL: archived manpage differs from man/axilio.1: %s\n' "$archive" >&2
		exit 1
	fi
done <"$archives"

cask="$dist/homebrew/Casks/axilio.rb"
[ -f "$cask" ] || {
	printf 'FAIL: generated cask is missing: %s\n' "$cask" >&2
	exit 1
}
binary_count=$(grep -Fxc '  binary "axilio"' "$cask" || true)
manpage_count=$(grep -Fxc '  manpage "man/axilio.1"' "$cask" || true)
[ "$binary_count" = 1 ] || {
	printf 'FAIL: generated cask does not contain exactly one axilio binary stanza\n' >&2
	exit 1
}
[ "$manpage_count" = 1 ] || {
	printf 'FAIL: generated cask does not contain exactly one axilio manpage stanza\n' >&2
	exit 1
}
binary_line=$(grep -Fn '  binary "axilio"' "$cask" | cut -d: -f1)
manpage_line=$(grep -Fn '  manpage "man/axilio.1"' "$cask" | cut -d: -f1)
[ "$binary_line" -lt "$manpage_line" ] || {
	printf 'FAIL: generated cask manpage stanza must follow its binary stanza\n' >&2
	exit 1
}
grep -F 'com.apple.quarantine' "$cask" >/dev/null || {
	printf 'FAIL: generated cask lost the quarantine hook\n' >&2
	exit 1
}
for target in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
	asset=$(awk -v target="_${target}.tar.gz" 'index($2, target) { print $2; exit }' "$checksums")
	release_sha=$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$checksums")
	cask_sha=$(awk -v target="${target}.tar.gz" '
		$1 == "sha256" { hash=$2; gsub(/"/, "", hash) }
		index($0, target) { print hash; exit }
	' "$cask")
	if [ -z "$asset" ] || [ -z "$release_sha" ] || [ "$cask_sha" != "$release_sha" ]; then
		printf 'FAIL: generated cask hash does not match %s\n' "${target}" >&2
		exit 1
	fi
done
ruby -c "$cask" >/dev/null

printf 'release archive and cask fixtures: PASS\n'
