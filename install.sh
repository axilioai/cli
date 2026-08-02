#!/bin/sh
# Install the Axilio CLI — https://github.com/axilioai/cli
#
#   curl -fsSL https://axilio.ai/install.sh | sh
#
# Downloads the latest release for your OS/arch, verifies its checksum, and
# installs the `axilio` binary onto your PATH and, for conventional prefixes,
# installs its manual page. Environment overrides:
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
	[ -n "$want" ] || err "no checksum listed for $archive"
	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$tmp/$archive" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
	else
		err "sha256sum or shasum is required to verify $archive"
	fi
	[ "$got" = "$want" ] || err "checksum mismatch for $archive"
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
while [ "$dir" != "/" ] && [ "${dir%/}" != "$dir" ]; do
	dir=${dir%/}
done

# --- install (elevate only when needed) -----------------------------------
if [ ! -d "$dir" ]; then
	if mkdir -p "$dir" 2>/dev/null; then
		:
	elif command -v sudo >/dev/null 2>&1; then
		sudo mkdir -p "$dir" || err "cannot create $dir with elevated permissions"
	else
		err "cannot create $dir and sudo is unavailable; set INSTALL_DIR to a writable path"
	fi
fi

if [ -w "$dir" ]; then
	install -m 0755 "$tmp/$BIN" "$dir/$BIN"
elif command -v sudo >/dev/null 2>&1; then
	info "Writing to $dir needs elevated permissions…"
	sudo install -m 0755 "$tmp/$BIN" "$dir/$BIN"
else
	err "cannot write to $dir and sudo is unavailable; set INSTALL_DIR to a writable path"
fi

info "Installed $BIN $version to $dir/$BIN"

# --- install manual page (best effort) ------------------------------------
man_source="$tmp/man/$BIN.1"
if [ ! -f "$man_source" ]; then
	warn "$archive does not contain man/$BIN.1; the binary is installed without a manual page"
else
	man_dir=""
	if [ "${MAN_DIR+x}" = x ]; then
		if [ -n "$MAN_DIR" ]; then
			man_dir="$MAN_DIR"
			while [ "$man_dir" != "/" ] && [ "${man_dir%/}" != "$man_dir" ]; do
				man_dir=${man_dir%/}
			done
		else
			warn "MAN_DIR is empty; the binary is installed without a manual page"
		fi
	else
		case "$dir" in
		/*/bin) man_dir="${dir%/bin}/share/man/man1" ;;
		/*/sbin) man_dir="${dir%/sbin}/share/man/man1" ;;
		*)
			warn "cannot infer a manual directory from INSTALL_DIR '$dir'; set MAN_DIR explicitly"
			;;
		esac
	fi

	if [ -n "$man_dir" ]; then
		man_ready=true
		if [ ! -d "$man_dir" ]; then
			if mkdir -p "$man_dir" 2>/dev/null; then
				:
			elif command -v sudo >/dev/null 2>&1; then
				if ! sudo mkdir -p "$man_dir"; then
					warn "cannot create manual directory $man_dir; the binary remains installed"
					man_ready=false
				fi
			else
				warn "cannot create manual directory $man_dir and sudo is unavailable; the binary remains installed"
				man_ready=false
			fi
		fi

		if [ "$man_ready" = true ]; then
			if [ -w "$man_dir" ]; then
				if ! install -m 0644 "$man_source" "$man_dir/$BIN.1"; then
					warn "cannot install $man_dir/$BIN.1; the binary remains installed"
					man_ready=false
				fi
			elif command -v sudo >/dev/null 2>&1; then
				if ! sudo install -m 0644 "$man_source" "$man_dir/$BIN.1"; then
					warn "cannot install $man_dir/$BIN.1 with elevated permissions; the binary remains installed"
					man_ready=false
				fi
			else
				warn "cannot write to manual directory $man_dir and sudo is unavailable; the binary remains installed"
				man_ready=false
			fi
		fi

		if [ "$man_ready" = true ]; then
			info "Installed manual page to $man_dir/$BIN.1"
		fi
	fi
fi

case ":$PATH:" in
*":$dir:"*) ;;
*) info "note: $dir is not on your PATH — add it, e.g.  export PATH=\"$dir:\$PATH\"" ;;
esac

"$dir/$BIN" --version 2>/dev/null || true
