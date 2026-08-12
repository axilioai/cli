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
mkdir -p "$fixtures" "$payload/man" "$payload/completions"
printf '%s\n' '#!/bin/sh' 'printf "%s\n" "axilio version v0.0.0"' >"$payload/axilio"
printf '%s\n' '# Axilio test release' >"$payload/README.md"
printf '%s\n' '.TH AXILIO 1' '.SH NAME' 'axilio \- fixture manual' >"$payload/man/axilio.1"
printf '%s\n' '<!doctype html>' '<title>Axilio fixture manual</title>' >"$payload/man/axilio.1.html"
printf '%s\n' '# axilio bash completion fixture' >"$payload/completions/axilio.bash"
printf '%s\n' '#compdef axilio' '# axilio zsh completion fixture' >"$payload/completions/_axilio"
printf '%s\n' '# axilio fish completion fixture' >"$payload/completions/axilio.fish"
chmod 0755 "$payload/axilio"
archive="$fixtures/axilio_0.0.0_linux_amd64.tar.gz"
tar -czf "$archive" -C "$payload" axilio README.md man/axilio.1 man/axilio.1.html \
	completions/axilio.bash completions/_axilio completions/axilio.fish
if command -v sha256sum >/dev/null 2>&1; then
	sum=$(sha256sum "$archive" | awk '{print $1}')
else
	sum=$(shasum -a 256 "$archive" | awk '{print $1}')
fi
printf '%s  %s\n' "$sum" "$(basename "$archive")" >"$fixtures/checksums.txt"
printf '%064d  %s\n' 0 "$(basename "$archive")" >"$fixtures/bad-checksums.txt"
printf '%064d  %s\n' 0 "axilio_0.0.0_linux_arm64.tar.gz" >"$fixtures/missing-checksums.txt"

# A pre-AXI-1578 release: same payload, no completions/ directory. Used to
# pin the warn-and-continue path for archives that predate completions.
legacy_archive="$fixtures/axilio_0.0.1_linux_amd64.tar.gz"
tar -czf "$legacy_archive" -C "$payload" axilio README.md man/axilio.1 man/axilio.1.html
if command -v sha256sum >/dev/null 2>&1; then
	legacy_sum=$(sha256sum "$legacy_archive" | awk '{print $1}')
else
	legacy_sum=$(shasum -a 256 "$legacy_archive" | awk '{print $1}')
fi
printf '%s  %s\n' "$legacy_sum" "$(basename "$legacy_archive")" >"$fixtures/legacy-checksums.txt"

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
# The hash tool is wrapped, not symlinked: macOS's shasum is a perl shim
# that resolves its versioned sibling relative to $0, so a symlink into this
# sandbox breaks it. An exec wrapper keeps $0 at the real path.
if command -v sha256sum >/dev/null 2>&1; then
	hash_tool=sha256sum
else
	hash_tool=shasum
fi
printf '%s\n' '#!/bin/sh' "exec $(command -v "$hash_tool") \"\$@\"" >"$tools/$hash_tool"
chmod 0755 "$tools/$hash_tool"

# Stub shells: the installer gates inferred completion directories on the
# shell being present, so the default toolchain claims all three exist.
for stub_shell in bash zsh fish; do
	printf '%s\n' '#!/bin/sh' 'exit 0' >"$tools/$stub_shell"
	chmod 0755 "$tools/$stub_shell"
done

# The same toolchain without any shells, to pin that completions are skipped
# (not warned about, not half-installed) when no shell is present.
tools_no_shells="$tmp/tools-no-shells"
mkdir -p "$tools_no_shells"
for entry in "$tools"/*; do
	case "${entry##*/}" in
	bash | zsh | fish) ;;
	*) ln -s "$entry" "$tools_no_shells/${entry##*/}" 2>/dev/null || cp "$entry" "$tools_no_shells/${entry##*/}" ;;
	esac
done

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

# run_install NAME INSTALL_DIR MAN_DIR VERSION CHECKSUMS [TOOLS] [BASH_DIR] [ZSH_DIR] [FISH_DIR]
# Use "-" for an unset optional value. The three completion overrides accept
# "" (set-but-empty, meaning skip that shell) as distinct from "-" (unset).
run_install() {
	name=$1
	install_dir=$2
	man_override=$3
	install_version=$4
	checksums=$5
	run_tools=${6:-$tools}
	# ${N--} (no colon): default only when the argument is genuinely unset,
	# so an explicit "" survives as set-but-empty (= skip that shell).
	bash_override=${7--}
	zsh_override=${8--}
	fish_override=${9--}
	log="$tmp/$name.log"
	(
		unset MAN_DIR VERSION INSTALL_TEST_CHECKSUMS
		unset BASH_COMPLETION_DIR ZSH_COMPLETION_DIR FISH_COMPLETION_DIR
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
		if [ "$bash_override" != - ]; then
			BASH_COMPLETION_DIR=$bash_override
			export BASH_COMPLETION_DIR
		fi
		if [ "$zsh_override" != - ]; then
			ZSH_COMPLETION_DIR=$zsh_override
			export ZSH_COMPLETION_DIR
		fi
		if [ "$fish_override" != - ]; then
			FISH_COMPLETION_DIR=$fish_override
			export FISH_COMPLETION_DIR
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
assert_file "$standard/share/man/man1/axilio.1.html"
assert_mode 755 "$standard/bin/axilio"
assert_mode 644 "$standard/share/man/man1/axilio.1"
assert_mode 644 "$standard/share/man/man1/axilio.1.html"
cmp "$payload/man/axilio.1" "$standard/share/man/man1/axilio.1" >/dev/null || fail "standard manual differs from archive"
cmp "$payload/man/axilio.1.html" "$standard/share/man/man1/axilio.1.html" >/dev/null || fail "standard HTML manual differs from archive"
assert_log "Downloading axilio v0.0.0" "$tmp/standard.log"
# The same prefix infers all three completion directories (the toolchain
# claims bash/zsh/fish all exist).
assert_file "$standard/share/bash-completion/completions/axilio"
assert_file "$standard/share/zsh/site-functions/_axilio"
assert_file "$standard/share/fish/vendor_completions.d/axilio.fish"
assert_mode 644 "$standard/share/bash-completion/completions/axilio"
assert_mode 644 "$standard/share/zsh/site-functions/_axilio"
assert_mode 644 "$standard/share/fish/vendor_completions.d/axilio.fish"
cmp "$payload/completions/axilio.bash" "$standard/share/bash-completion/completions/axilio" >/dev/null || fail "standard bash completion differs from archive"
cmp "$payload/completions/_axilio" "$standard/share/zsh/site-functions/_axilio" >/dev/null || fail "standard zsh completion differs from archive"
cmp "$payload/completions/axilio.fish" "$standard/share/fish/vendor_completions.d/axilio.fish" >/dev/null || fail "standard fish completion differs from archive"

# A rerun replaces stale contents and restores the intended modes.
printf '%s\n' stale >"$standard/bin/axilio"
printf '%s\n' stale >"$standard/share/man/man1/axilio.1"
printf '%s\n' stale >"$standard/share/man/man1/axilio.1.html"
chmod 0600 "$standard/bin/axilio" "$standard/share/man/man1/axilio.1" "$standard/share/man/man1/axilio.1.html"
run_install rerun "$standard/bin/" - v0.0.0 -
cmp "$payload/axilio" "$standard/bin/axilio" >/dev/null || fail "rerun did not replace binary"
cmp "$payload/man/axilio.1" "$standard/share/man/man1/axilio.1" >/dev/null || fail "rerun did not replace manual"
cmp "$payload/man/axilio.1.html" "$standard/share/man/man1/axilio.1.html" >/dev/null || fail "rerun did not replace HTML manual"
assert_mode 755 "$standard/bin/axilio"
assert_mode 644 "$standard/share/man/man1/axilio.1"
assert_mode 644 "$standard/share/man/man1/axilio.1.html"

# A user-local bin directory has the same prefix contract. fish diverges by
# design: a home-prefix install goes to the user config dir fish auto-loads,
# and the zsh destination earns an fpath hint since only /usr and /usr/local
# are on the default fpath.
local_prefix="$test_home/.local"
run_install local "$local_prefix/bin" - v0.0.0 -
assert_file "$local_prefix/bin/axilio"
assert_file "$local_prefix/share/man/man1/axilio.1"
assert_file "$local_prefix/share/man/man1/axilio.1.html"
assert_file "$local_prefix/share/bash-completion/completions/axilio"
assert_file "$local_prefix/share/zsh/site-functions/_axilio"
assert_file "$test_home/.config/fish/completions/axilio.fish"
assert_no_file "$local_prefix/share/fish/vendor_completions.d/axilio.fish"
assert_log "on your zsh fpath" "$tmp/local.log"

# sbin is also a conventional executable directory.
sbin_prefix="$tmp/opt/axilio"
run_install sbin "$sbin_prefix/sbin" - v0.0.0 -
assert_file "$sbin_prefix/sbin/axilio"
assert_file "$sbin_prefix/share/man/man1/axilio.1"
assert_file "$sbin_prefix/share/man/man1/axilio.1.html"

# MAN_DIR is authoritative, including with an otherwise arbitrary binary path.
explicit_bin="$tmp/custom/tools"
explicit_man="$tmp/custom/manuals/section-one"
run_install explicit "$explicit_bin" "$explicit_man" v0.0.0 -
assert_file "$explicit_bin/axilio"
assert_file "$explicit_man/axilio.1"
assert_file "$explicit_man/axilio.1.html"
assert_mode 644 "$explicit_man/axilio.1"
assert_mode 644 "$explicit_man/axilio.1.html"

# Arbitrary layouts do not invite prefix guessing; the binary still succeeds.
arbitrary="$tmp/arbitrary/executables"
run_install arbitrary "$arbitrary" - v0.0.0 -
assert_file "$arbitrary/axilio"
assert_no_file "$tmp/arbitrary/share/man/man1/axilio.1"
assert_no_file "$tmp/arbitrary/share/man/man1/axilio.1.html"
assert_log "cannot infer a manual directory" "$tmp/arbitrary.log"
assert_log "set MAN_DIR explicitly" "$tmp/arbitrary.log"
assert_no_file "$tmp/arbitrary/share/bash-completion/completions/axilio"
assert_log "cannot infer completion directories" "$tmp/arbitrary.log"

# Explicit completion overrides are authoritative, whatever the binary path.
comp_bash="$tmp/custom/comp-bash"
comp_zsh="$tmp/custom/comp-zsh"
comp_fish="$tmp/custom/comp-fish"
run_install comp-explicit "$tmp/custom/comp-tools" - v0.0.0 - "$tools" "$comp_bash" "$comp_zsh" "$comp_fish"
assert_file "$tmp/custom/comp-tools/axilio"
assert_file "$comp_bash/axilio"
assert_file "$comp_zsh/_axilio"
assert_file "$comp_fish/axilio.fish"
assert_mode 644 "$comp_bash/axilio"
assert_mode 644 "$comp_zsh/_axilio"
assert_mode 644 "$comp_fish/axilio.fish"

# Set-but-empty overrides skip their shell even under a conventional prefix,
# and suppress the cannot-infer warning (the choice was explicit).
skip_prefix="$tmp/skip-comp"
run_install comp-skip "$skip_prefix/bin" - v0.0.0 - "$tools" "" "" ""
assert_file "$skip_prefix/bin/axilio"
assert_no_file "$skip_prefix/share/bash-completion/completions/axilio"
assert_no_file "$skip_prefix/share/zsh/site-functions/_axilio"
assert_no_file "$skip_prefix/share/fish/vendor_completions.d/axilio.fish"
if grep -F "cannot infer completion directories" "$tmp/comp-skip.log" >/dev/null; then
	fail "explicit empty overrides still warned about inference"
fi

# Without any shell on the PATH, inferred completions are skipped quietly.
no_shell_prefix="$tmp/no-shells-prefix"
run_install no-shells "$no_shell_prefix/bin" - v0.0.0 - "$tools_no_shells"
assert_file "$no_shell_prefix/bin/axilio"
assert_file "$no_shell_prefix/share/man/man1/axilio.1"
assert_no_file "$no_shell_prefix/share/bash-completion/completions/axilio"
assert_no_file "$no_shell_prefix/share/zsh/site-functions/_axilio"
assert_no_file "$no_shell_prefix/share/fish/vendor_completions.d/axilio.fish"

# An archive that predates completions warns once and installs everything else.
legacy_prefix="$tmp/legacy"
run_install legacy "$legacy_prefix/bin" - v0.0.1 "$fixtures/legacy-checksums.txt"
assert_file "$legacy_prefix/bin/axilio"
assert_file "$legacy_prefix/share/man/man1/axilio.1"
assert_no_file "$legacy_prefix/share/bash-completion/completions/axilio"
assert_log "does not contain completions/" "$tmp/legacy.log"

# A structurally unwritable man destination and no sudo is best-effort only.
blocked_prefix="$tmp/blocked-man"
mkdir -p "$blocked_prefix/bin"
printf '%s\n' blocker >"$blocked_prefix/share"
run_install blocked-man "$blocked_prefix/bin" - v0.0.0 -
assert_file "$blocked_prefix/bin/axilio"
assert_no_file "$blocked_prefix/share/man/man1/axilio.1"
assert_no_file "$blocked_prefix/share/man/man1/axilio.1.html"
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
assert_no_file "$bad_prefix/share/man/man1/axilio.1.html"
assert_log "checksum mismatch" "$tmp/bad-checksum.log"

# A published checksum file that omits this platform is not permission to run
# unverified bytes.
missing_prefix="$tmp/missing-checksum"
if run_install missing-checksum "$missing_prefix/bin" - v0.0.0 "$fixtures/missing-checksums.txt"; then
	fail "installer accepted a checksum file without the selected archive"
fi
assert_no_file "$missing_prefix/bin/axilio"
assert_log "no checksum listed" "$tmp/missing-checksum.log"

# An unreachable checksums.txt is also not permission to run unverified bytes:
# every release publishes one, so a failed fetch means something is wrong.
unfetchable_prefix="$tmp/unfetchable-checksum"
if run_install unfetchable-checksum "$unfetchable_prefix/bin" - v0.0.0 "$fixtures/absent-checksums.txt"; then
	fail "installer accepted an archive whose checksums.txt could not be downloaded"
fi
assert_no_file "$unfetchable_prefix/bin/axilio"
assert_log "refusing to install unverified bytes" "$tmp/unfetchable-checksum.log"

# Likewise, a host without a supported SHA-256 utility must stop rather than
# silently treating the downloaded archive as verified.
no_hash_prefix="$tmp/no-hash"
if run_install no-hash "$no_hash_prefix/bin" - v0.0.0 - "$tools_no_hash"; then
	fail "installer accepted an archive without a SHA-256 utility"
fi
assert_no_file "$no_hash_prefix/bin/axilio"
assert_log "sha256sum or shasum is required" "$tmp/no-hash.log"

printf 'install.sh fixtures: PASS\n'
