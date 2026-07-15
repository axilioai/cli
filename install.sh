#!/bin/sh
# Install the Axilio CLI — https://github.com/axilioai/cli
#
#   curl -fsSL https://axilio.ai/install.sh | sh
#
# Downloads the latest release for your OS/arch, verifies its checksum, and
# installs the `axilio` binary onto your PATH. Environment overrides:
#
#   VERSION       release tag to install (default: latest, e.g. VERSION=v0.1.0)
#   INSTALL_DIR   target directory (default: /usr/local/bin, else ~/.local/bin)
set -eu

REPO="axilioai/cli"
BIN="axilio"

info() { printf '%s\n' "$*" >&2; }
err() {
	printf 'error: %s\n' "$*" >&2
	exit 1
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

# --- verify checksum (fail closed when a sum is published) ---------------
if curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
	want=$(grep " $archive\$" "$tmp/checksums.txt" 2>/dev/null | awk '{print $1}' | head -n1)
	if [ -n "$want" ]; then
		if command -v sha256sum >/dev/null 2>&1; then
			got=$(sha256sum "$tmp/$archive" | awk '{print $1}')
		elif command -v shasum >/dev/null 2>&1; then
			got=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
		else
			got=""
		fi
		[ -z "$got" ] || [ "$got" = "$want" ] || err "checksum mismatch for $archive"
	fi
fi

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
mkdir -p "$dir" 2>/dev/null || true

# --- install (elevate only when needed) -----------------------------------
if [ -w "$dir" ]; then
	install -m 0755 "$tmp/$BIN" "$dir/$BIN"
elif command -v sudo >/dev/null 2>&1; then
	info "Writing to $dir needs elevated permissions…"
	sudo install -m 0755 "$tmp/$BIN" "$dir/$BIN"
else
	err "cannot write to $dir and sudo is unavailable; set INSTALL_DIR to a writable path"
fi

info "Installed $BIN $version to $dir/$BIN"
case ":$PATH:" in
*":$dir:"*) ;;
*) info "note: $dir is not on your PATH — add it, e.g.  export PATH=\"$dir:\$PATH\"" ;;
esac

"$dir/$BIN" --version 2>/dev/null || true
