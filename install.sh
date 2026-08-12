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
#   BASH_COMPLETION_DIR / ZSH_COMPLETION_DIR / FISH_COMPLETION_DIR
#                 completion directories. Defaults are inferred from the same
#                 prefix, per shell present on the system; set one explicitly
#                 to force it, or set it empty to skip that shell. Shell rc
#                 files are never touched.
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

# --- install completions (best effort; never undoes the installed binary) --
# Release archives carry completions/ since AXI-1578. The same rules as the
# manuals apply: an explicit *_COMPLETION_DIR is authoritative (empty skips
# that shell), otherwise a directory is inferred from the executable prefix
# for each shell actually present on the system. Failures warn and continue;
# shell startup files are never modified.

# install_completion SHELL SRC DEST_DIR NAME -> 0 installed, 1 skipped/failed
install_completion() {
	ic_shell=$1
	ic_src=$2
	ic_dir=$3
	ic_name=$4
	if [ ! -f "$ic_src" ]; then
		warn "$archive does not contain completions/${ic_src##*/}; skipping $ic_shell completion"
		return 1
	fi
	if ! ensure_dir "$ic_dir"; then
		warn "cannot create $ic_shell completion directory $ic_dir $(sudo_note); the binary remains installed"
		return 1
	fi
	if put_file 0644 "$ic_src" "$ic_dir" "$ic_name"; then
		info "Installed $ic_shell completion to $ic_dir/$ic_name"
		return 0
	fi
	warn "cannot install $ic_dir/$ic_name $(sudo_note); the binary remains installed"
	return 1
}

comp_src_dir="$tmp/completions"
comp_prefix=""
case "$dir" in
/*/bin) comp_prefix="${dir%/bin}" ;;
/*/sbin) comp_prefix="${dir%/sbin}" ;;
esac

if [ ! -d "$comp_src_dir" ]; then
	warn "$archive does not contain completions/; the binary is installed without shell completions"
else
	comp_any_override=""
	if [ "${BASH_COMPLETION_DIR+x}" = x ] || [ "${ZSH_COMPLETION_DIR+x}" = x ] || [ "${FISH_COMPLETION_DIR+x}" = x ]; then
		comp_any_override=1
	fi
	if [ -z "$comp_prefix" ] && [ -z "$comp_any_override" ]; then
		warn "cannot infer completion directories from INSTALL_DIR '$dir'; set BASH_COMPLETION_DIR, ZSH_COMPLETION_DIR, or FISH_COMPLETION_DIR explicitly"
	fi

	# bash: bash-completion v2 loads on demand from the prefix's
	# share/bash-completion/completions (the ~/.local prefix is its XDG path).
	if [ "${BASH_COMPLETION_DIR+x}" = x ]; then
		if [ -n "$BASH_COMPLETION_DIR" ]; then
			install_completion bash "$comp_src_dir/$BIN.bash" "$(trim_slashes "$BASH_COMPLETION_DIR")" "$BIN" || true
		fi
	elif [ -n "$comp_prefix" ] && command -v bash >/dev/null 2>&1; then
		install_completion bash "$comp_src_dir/$BIN.bash" "$comp_prefix/share/bash-completion/completions" "$BIN" || true
	fi

	# zsh: site-functions under the prefix. /usr and /usr/local are on the
	# default fpath; any other prefix gets a hint (we never edit rc files).
	if [ "${ZSH_COMPLETION_DIR+x}" = x ]; then
		if [ -n "$ZSH_COMPLETION_DIR" ]; then
			install_completion zsh "$comp_src_dir/_$BIN" "$(trim_slashes "$ZSH_COMPLETION_DIR")" "_$BIN" || true
		fi
	elif [ -n "$comp_prefix" ] && command -v zsh >/dev/null 2>&1; then
		if install_completion zsh "$comp_src_dir/_$BIN" "$comp_prefix/share/zsh/site-functions" "_$BIN"; then
			case "$comp_prefix" in
			/usr | /usr/local) ;;
			*) info "note: ensure $comp_prefix/share/zsh/site-functions is on your zsh fpath (before compinit)" ;;
			esac
		fi
	fi

	# fish: the user config dir for a home-prefix install (always auto-loaded);
	# vendor_completions.d under the prefix otherwise.
	if [ "${FISH_COMPLETION_DIR+x}" = x ]; then
		if [ -n "$FISH_COMPLETION_DIR" ]; then
			install_completion fish "$comp_src_dir/$BIN.fish" "$(trim_slashes "$FISH_COMPLETION_DIR")" "$BIN.fish" || true
		fi
	elif [ -n "$comp_prefix" ] && command -v fish >/dev/null 2>&1; then
		if [ "$comp_prefix" = "$HOME/.local" ]; then
			fish_dir="$HOME/.config/fish/completions"
		else
			fish_dir="$comp_prefix/share/fish/vendor_completions.d"
		fi
		install_completion fish "$comp_src_dir/$BIN.fish" "$fish_dir" "$BIN.fish" || true
	fi
fi

case ":$PATH:" in
*":$dir:"*) ;;
*) info "note: $dir is not on your PATH — add it, e.g.  export PATH=\"$dir:\$PATH\"" ;;
esac

"$dir/$BIN" --version 2>/dev/null || true
