#!/usr/bin/env bash
set -euo pipefail

mode=install
if [ "${1:-}" = "--check" ]; then
  mode=check
  shift
fi
if [ "$#" -ne 0 ]; then
  echo "usage: scripts/install-git-hooks.sh [--check]" >&2
  exit 2
fi

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

origin_url=$(git config --get remote.origin.url || true)
case "$origin_url" in
  https://github.com/axilioai/cli|https://github.com/axilioai/cli.git|\
  https://github.com/axilioai/cli-public-rebuild-2026-08-03|https://github.com/axilioai/cli-public-rebuild-2026-08-03.git|\
  git@github.com:axilioai/cli|git@github.com:axilioai/cli.git|\
  git@github.com:axilioai/cli-public-rebuild-2026-08-03|git@github.com:axilioai/cli-public-rebuild-2026-08-03.git|\
  ssh://git@github.com/axilioai/cli|ssh://git@github.com/axilioai/cli.git|\
  ssh://git@github.com/axilioai/cli-public-rebuild-2026-08-03|ssh://git@github.com/axilioai/cli-public-rebuild-2026-08-03.git)
    ;;
  *)
    echo "origin is not the Axilio CLI replacement repository" >&2
    exit 1
    ;;
esac

effective_hooks_path=$(git config --get core.hooksPath || true)
local_hooks_path=$(git config --local --get core.hooksPath || true)
if [ -n "$effective_hooks_path" ] && [ "$effective_hooks_path" != ".githooks" ]; then
  echo "another core.hooksPath is active; chain .githooks/pre-push manually and rerun with --check" >&2
  exit 1
fi

if [ ! -x .githooks/pre-push ] || [ ! -x .github/scripts/check-git-identities.sh ]; then
  echo "versioned identity hooks are missing or not executable" >&2
  exit 1
fi

if ! tracking_oid=$(git rev-parse --verify refs/remotes/origin/main 2>/dev/null); then
  echo "origin/main is missing; fetch origin before installing hooks" >&2
  exit 1
fi

if ! remote_record=$(git ls-remote --exit-code origin refs/heads/main); then
  echo "origin/main could not be verified against the live remote" >&2
  exit 1
fi
remote_oid=${remote_record%%[[:space:]]*}
if [ "$tracking_oid" != "$remote_oid" ]; then
  echo "origin/main is stale; fetch origin and rerun the installer" >&2
  exit 1
fi

if ! git cat-file -e "refs/remotes/origin/main:.github/git-identity-allowlist.txt" 2>/dev/null; then
  echo "the trusted identity policy is missing from origin/main" >&2
  exit 1
fi

if [ "$mode" = "check" ]; then
  pinned_origin=$(git config --local --get axilio.identityPolicyOrigin || true)
  if [ "$local_hooks_path" != ".githooks" ] || [ "$pinned_origin" != "$origin_url" ]; then
    echo "identity hook configuration is incomplete; run scripts/install-git-hooks.sh" >&2
    exit 1
  fi
  echo "identity hook doctor passed"
  exit 0
fi

git config --local core.hooksPath .githooks
git config --local axilio.identityPolicyOrigin "$origin_url"
echo "installed versioned Git hooks for $origin_url"
