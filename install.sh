#!/bin/sh
# Install the Axilio CLI — https://github.com/axilioai/cli
#
#   curl -fsSL https://axilio.ai/install.sh | sh
#
# Downloads the latest release for your OS/arch, verifies its checksum, and
# installs the `axilio` binary onto your PATH and, for conventional prefixes,
# installs its terminal and HTML manuals. Environment overrides:
#
#   VERSION       release tag to install (default: latest, e.g. VERSION=v0.6.1)
#   INSTALL_DIR   target directory (default: /usr/local/bin, else ~/.local/bin)
#   MAN_DIR       manual directory (default: <prefix>/share/man/man1 when
#                 INSTALL_DIR ends in /bin or /sbin)
set -eu

REPO="axilioai/cli"
BIN="axilio"

info() { printf '%s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
err() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

# --- write helpers --------------------------------------------------------
# ensure_dir and put_file return non-zero rather than exiting, so each caller
# decides whether a failure is fatal (the binary) or best effort (manuals).
# sudo is attempted only after the unprivileged path fails.
have_sudo() { command -v sudo >/dev/null 2>&1; }

# Reason suffix for a failure message, accurate whether or not sudo exists.
sudo_note() {
	if have_sudo; then
		printf 'even with elevated permissions'
	else
		printf 'and sudo is unavailable'
	fi
}

trim_slashes() {
	ts_value=$1
	while [ "$ts_value" != "/" ] && [ "${ts_value%/}" != "$ts_value" ]; do
		ts_value=${ts_value%/}
	done
	printf '%s' "$ts_value"
}

ensure_dir() {
	[ -d "$1" ] && return 0
	mkdir -p "$1" 2>/dev/null && return 0
	have_sudo || return 1
	sudo mkdir -p "$1" 2>/dev/null
}

# put_file MODE SRC DEST_DIR NAME
put_file() {
	pf_mode=$1
	pf_src=$2
	pf_dir=$3
	pf_name=$4
	if [ -w "$pf_dir" ] && install -m "$pf_mode" "$pf_src" "$pf_dir/$pf_name" 2>/dev/null; then
		return 0
	fi
	have_sudo || return 1
	info "Writing to $pf_dir needs elevated permissions…"
	sudo install -m "$pf_mode" "$pf_src" "$pf_dir/$pf_name" 2>/dev/null
}

# --- platform -------------------------------------------------------------
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) err "unsupported architecture: $arch (try: go install $REPO/cmd/$BIN@latest)" ;;
esac
case "$os" in
darwin | linux) ;;
*) err "unsupported OS: $os (try Homebrew or: go install $REPO/cmd/$BIN@latest)" ;;
esac

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v tar >/dev/null 2>&1 || err "tar is required"

# --- resolve version ------------------------------------------------------
version="${VERSION:-}"
if [ -z "$version" ]; then
	version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
fi
[ -n "$version" ] || err "could not determine the latest release"

num="${version#v}"
archive="${BIN}_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

info "Downloading $BIN $version ($os/$arch)…"
curl -fsSL "$base/$archive" -o "$tmp/$archive" || err "download failed: $base/$archive"

# --- verify checksum (always; every release publishes checksums.txt) ------
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null ||
	err "could not download $base/checksums.txt; refusing to install unverified bytes"
want=$(grep " $archive\$" "$tmp/checksums.txt" 2>/dev/null | awk '{print $1}' | head -n1)
[ -n "$want" ] || err "no checksum listed for $archive"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else
	err "sha256sum or shasum is required to verify $archive"
fi
[ "$got" = "$want" ] || err "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" || err "failed to extract $archive"
[ -f "$tmp/$BIN" ] || err "$BIN not found in the archive"
chmod +x "$tmp/$BIN"

# --- choose install dir ---------------------------------------------------
dir="${INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		dir=/usr/local/bin
	else
		dir="$HOME/.local/bin"
	fi
fi
dir=$(trim_slashes "$dir")

# --- install (elevate only when needed) -----------------------------------
ensure_dir "$dir" ||
	err "cannot create $dir $(sudo_note); set INSTALL_DIR to a writable path"
put_file 0755 "$tmp/$BIN" "$dir" "$BIN" ||
	err "cannot write to $dir $(sudo_note); set INSTALL_DIR to a writable path"

info "Installed $BIN $version to $dir/$BIN"

# --- install manuals (best effort; never undoes the installed binary) -----
man_source="$tmp/man/$BIN.1"
html_man_source="$tmp/man/$BIN.1.html"
man_dir=""
if [ ! -f "$man_source" ]; then
	warn "$archive does not contain man/$BIN.1; the binary is installed without offline manuals"
elif [ "${MAN_DIR+x}" = x ]; then
	if [ -n "$MAN_DIR" ]; then
		man_dir=$(trim_slashes "$MAN_DIR")
	else
		warn "MAN_DIR is empty; the binary is installed without offline manuals"
	fi
else
	# Only a conventional executable prefix implies a manual destination.
	case "$dir" in
	/*/bin) man_dir="${dir%/bin}/share/man/man1" ;;
	/*/sbin) man_dir="${dir%/sbin}/share/man/man1" ;;
	*) warn "cannot infer a manual directory from INSTALL_DIR '$dir'; set MAN_DIR explicitly" ;;
	esac
fi

if [ -n "$man_dir" ]; then
	if ! ensure_dir "$man_dir"; then
		warn "cannot create manual directory $man_dir $(sudo_note); the binary remains installed"
	elif ! put_file 0644 "$man_source" "$man_dir" "$BIN.1"; then
		warn "cannot install $man_dir/$BIN.1 $(sudo_note); the binary remains installed"
	else
		info "Installed manual page to $man_dir/$BIN.1"
		if [ ! -f "$html_man_source" ]; then
			warn "$archive does not contain man/$BIN.1.html; the terminal manual remains installed"
		elif put_file 0644 "$html_man_source" "$man_dir" "$BIN.1.html"; then
			info "Installed HTML manual to $man_dir/$BIN.1.html"
		else
			warn "cannot install $man_dir/$BIN.1.html $(sudo_note); the terminal manual remains installed"
		fi
	fi
fi

case ":$PATH:" in
*":$dir:"*) ;;
*) info "note: $dir is not on your PATH — add it, e.g.  export PATH=\"$dir:\$PATH\"" ;;
esac

"$dir/$BIN" --version 2>/dev/null || true
