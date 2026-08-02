#!/bin/sh
# Exercise install.sh without network, sudo, or writes outside a throwaway tree.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
installer="$root/install.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

assert_file() {
	[ -f "$1" ] || fail "missing file: $1"
}

assert_no_file() {
	[ ! -e "$1" ] || fail "unexpected file: $1"
}

assert_mode() {
	want=$1
	file=$2
	got=$(stat -c '%a' "$file" 2>/dev/null || stat -f '%Lp' "$file")
	[ "$got" = "$want" ] || fail "$file mode is $got, want $want"
}

assert_log() {
	pattern=$1
	file=$2
	grep -F "$pattern" "$file" >/dev/null || fail "$file does not contain: $pattern"
}

fixtures="$tmp/fixtures"
payload="$tmp/payload"
mkdir -p "$fixtures" "$payload/man"
printf '%s\n' '#!/bin/sh' 'printf "%s\n" "axilio version v0.0.0"' >"$payload/axilio"
printf '%s\n' '# Axilio test release' >"$payload/README.md"
printf '%s\n' '.TH AXILIO 1' '.SH NAME' 'axilio \- fixture manual' >"$payload/man/axilio.1"
chmod 0755 "$payload/axilio"
archive="$fixtures/axilio_0.0.0_linux_amd64.tar.gz"
tar -czf "$archive" -C "$payload" axilio README.md man/axilio.1
if command -v sha256sum >/dev/null 2>&1; then
	sum=$(sha256sum "$archive" | awk '{print $1}')
else
	sum=$(shasum -a 256 "$archive" | awk '{print $1}')
fi
printf '%s  %s\n' "$sum" "$(basename "$archive")" >"$fixtures/checksums.txt"
printf '%064d  %s\n' 0 "$(basename "$archive")" >"$fixtures/bad-checksums.txt"
printf '%064d  %s\n' 0 "axilio_0.0.0_linux_arm64.tar.gz" >"$fixtures/missing-checksums.txt"

# Give the installer a deterministic toolchain with no sudo command. curl and
# uname are fixture implementations; the rest are links to ordinary host tools.
tools="$tmp/tools"
mkdir -p "$tools"
cp "$root/scripts/testdata/install/curl" "$tools/curl"
cp "$root/scripts/testdata/install/uname" "$tools/uname"
chmod 0755 "$tools/curl" "$tools/uname"
for tool in awk chmod cp grep gzip head install mkdir mktemp rm sed tar tr; do
	tool_path=$(command -v "$tool")
	ln -s "$tool_path" "$tools/$tool"
done
if command -v sha256sum >/dev/null 2>&1; then
	ln -s "$(command -v sha256sum)" "$tools/sha256sum"
else
	ln -s "$(command -v shasum)" "$tools/shasum"
fi

# Same deterministic toolchain without a SHA-256 implementation, used to pin
# the installer's fail-closed behavior when checksums are published.
tools_no_hash="$tmp/tools-no-hash"
mkdir -p "$tools_no_hash"
cp "$root/scripts/testdata/install/curl" "$tools_no_hash/curl"
cp "$root/scripts/testdata/install/uname" "$tools_no_hash/uname"
chmod 0755 "$tools_no_hash/curl" "$tools_no_hash/uname"
for tool in awk chmod cp grep gzip head install mkdir mktemp rm sed tar tr; do
	tool_path=$(command -v "$tool")
	ln -s "$tool_path" "$tools_no_hash/$tool"
done

test_home="$tmp/home"
mkdir -p "$test_home"
shell=$(command -v sh)

# run_install NAME INSTALL_DIR MAN_DIR VERSION CHECKSUMS
# Use "-" for an unset optional value.
run_install() {
	name=$1
	install_dir=$2
	man_override=$3
	install_version=$4
	checksums=$5
	run_tools=${6:-$tools}
	log="$tmp/$name.log"
	(
		unset MAN_DIR VERSION INSTALL_TEST_CHECKSUMS
		PATH=$run_tools
		HOME=$test_home
		INSTALL_TEST_FIXTURES=$fixtures
		INSTALL_DIR=$install_dir
		export PATH HOME INSTALL_TEST_FIXTURES INSTALL_DIR
		if [ "$man_override" != - ]; then
			MAN_DIR=$man_override
			export MAN_DIR
		fi
		if [ "$install_version" != - ]; then
			VERSION=$install_version
			export VERSION
		fi
		if [ "$checksums" != - ]; then
			INSTALL_TEST_CHECKSUMS=$checksums
			export INSTALL_TEST_CHECKSUMS
		fi
		"$shell" "$installer"
	) >"$log" 2>&1
}

# A /usr/local/bin-shaped prefix infers share/man/man1. Leaving VERSION unset
# also exercises the stubbed latest-release lookup.
standard="$tmp/usr/local"
run_install standard "$standard/bin" - - -
assert_file "$standard/bin/axilio"
assert_file "$standard/share/man/man1/axilio.1"
assert_mode 755 "$standard/bin/axilio"
assert_mode 644 "$standard/share/man/man1/axilio.1"
cmp "$payload/man/axilio.1" "$standard/share/man/man1/axilio.1" >/dev/null || fail "standard manual differs from archive"
assert_log "Downloading axilio v0.0.0" "$tmp/standard.log"

# A rerun replaces stale contents and restores the intended modes.
printf '%s\n' stale >"$standard/bin/axilio"
printf '%s\n' stale >"$standard/share/man/man1/axilio.1"
chmod 0600 "$standard/bin/axilio" "$standard/share/man/man1/axilio.1"
run_install rerun "$standard/bin/" - v0.0.0 -
cmp "$payload/axilio" "$standard/bin/axilio" >/dev/null || fail "rerun did not replace binary"
cmp "$payload/man/axilio.1" "$standard/share/man/man1/axilio.1" >/dev/null || fail "rerun did not replace manual"
assert_mode 755 "$standard/bin/axilio"
assert_mode 644 "$standard/share/man/man1/axilio.1"

# A user-local bin directory has the same prefix contract.
local_prefix="$test_home/.local"
run_install local "$local_prefix/bin" - v0.0.0 -
assert_file "$local_prefix/bin/axilio"
assert_file "$local_prefix/share/man/man1/axilio.1"

# sbin is also a conventional executable directory.
sbin_prefix="$tmp/opt/axilio"
run_install sbin "$sbin_prefix/sbin" - v0.0.0 -
assert_file "$sbin_prefix/sbin/axilio"
assert_file "$sbin_prefix/share/man/man1/axilio.1"

# MAN_DIR is authoritative, including with an otherwise arbitrary binary path.
explicit_bin="$tmp/custom/tools"
explicit_man="$tmp/custom/manuals/section-one"
run_install explicit "$explicit_bin" "$explicit_man" v0.0.0 -
assert_file "$explicit_bin/axilio"
assert_file "$explicit_man/axilio.1"
assert_mode 644 "$explicit_man/axilio.1"

# Arbitrary layouts do not invite prefix guessing; the binary still succeeds.
arbitrary="$tmp/arbitrary/executables"
run_install arbitrary "$arbitrary" - v0.0.0 -
assert_file "$arbitrary/axilio"
assert_no_file "$tmp/arbitrary/share/man/man1/axilio.1"
assert_log "cannot infer a manual directory" "$tmp/arbitrary.log"
assert_log "set MAN_DIR explicitly" "$tmp/arbitrary.log"

# A structurally unwritable man destination and no sudo is best-effort only.
blocked_prefix="$tmp/blocked-man"
mkdir -p "$blocked_prefix/bin"
printf '%s\n' blocker >"$blocked_prefix/share"
run_install blocked-man "$blocked_prefix/bin" - v0.0.0 -
assert_file "$blocked_prefix/bin/axilio"
assert_no_file "$blocked_prefix/share/man/man1/axilio.1"
assert_log "sudo is unavailable; the binary remains installed" "$tmp/blocked-man.log"

# A binary destination that cannot be created remains a hard failure.
blocked_bin="$tmp/blocked-bin"
printf '%s\n' blocker >"$blocked_bin"
if run_install blocked-bin "$blocked_bin/bin" - v0.0.0 -; then
	fail "installer succeeded with an unusable binary destination"
fi
assert_log "cannot create $blocked_bin/bin and sudo is unavailable" "$tmp/blocked-bin.log"

# Published checksum mismatches fail before either artifact is installed.
bad_prefix="$tmp/bad-checksum"
if run_install bad-checksum "$bad_prefix/bin" - v0.0.0 "$fixtures/bad-checksums.txt"; then
	fail "installer accepted a checksum mismatch"
fi
assert_no_file "$bad_prefix/bin/axilio"
assert_no_file "$bad_prefix/share/man/man1/axilio.1"
assert_log "checksum mismatch" "$tmp/bad-checksum.log"

# A published checksum file that omits this platform is not permission to run
# unverified bytes.
missing_prefix="$tmp/missing-checksum"
if run_install missing-checksum "$missing_prefix/bin" - v0.0.0 "$fixtures/missing-checksums.txt"; then
	fail "installer accepted a checksum file without the selected archive"
fi
assert_no_file "$missing_prefix/bin/axilio"
assert_log "no checksum listed" "$tmp/missing-checksum.log"

# Likewise, a host without a supported SHA-256 utility must stop rather than
# silently treating the downloaded archive as verified.
no_hash_prefix="$tmp/no-hash"
if run_install no-hash "$no_hash_prefix/bin" - v0.0.0 - "$tools_no_hash"; then
	fail "installer accepted an archive without a SHA-256 utility"
fi
assert_no_file "$no_hash_prefix/bin/axilio"
assert_log "sha256sum or shasum is required" "$tmp/no-hash.log"

printf 'install.sh fixtures: PASS\n'
