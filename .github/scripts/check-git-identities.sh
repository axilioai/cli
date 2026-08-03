#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: check-git-identities.sh --policy FILE [--inventory TSV] [--range REVSET] [--rev REV] [--all] [--active-email EMAIL]" >&2
}

policy_file=
inventory_file=
scan_all=false
ranges=()
revs=()
active_emails=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --policy)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      policy_file=$2
      shift 2
      ;;
    --range)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      ranges+=("$2")
      shift 2
      ;;
    --inventory)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      inventory_file=$2
      shift 2
      ;;
    --rev)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      revs+=("$2")
      shift 2
      ;;
    --all)
      scan_all=true
      shift
      ;;
    --active-email)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      active_emails+=("$2")
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [ -z "$policy_file" ] || [ ! -f "$policy_file" ]; then
  echo "identity policy is missing" >&2
  exit 1
fi
if [ -n "$inventory_file" ] && [ ! -f "$inventory_file" ]; then
  echo "identity inventory is missing" >&2
  exit 1
fi

if [ "$scan_all" = false ] && [ "${#ranges[@]}" -eq 0 ] && [ "${#revs[@]}" -eq 0 ] && [ "${#active_emails[@]}" -eq 0 ]; then
  usage
  exit 2
fi

if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
  echo "identity policy requires complete Git history" >&2
  exit 1
fi

identity_tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/git-identity-check.XXXXXX")
trap 'rm -rf "$identity_tmp_dir"' EXIT

normalized_policy="$identity_tmp_dir/policy.txt"
sorted_policy="$identity_tmp_dir/policy.sorted.txt"
commit_file="$identity_tmp_dir/commits.txt"
unique_commit_file="$identity_tmp_dir/commits.unique.txt"
ref_file="$identity_tmp_dir/refs.txt"
trailer_file="$identity_tmp_dir/trailers.txt"
observed_email_file="$identity_tmp_dir/observed-emails.txt"
inventory_email_file="$identity_tmp_dir/inventory-emails.txt"
: > "$commit_file"
: > "$observed_email_file"

awk '
  {
    sub(/^[[:space:]]+/, "")
    sub(/[[:space:]]+$/, "")
  }
  $0 != "" && $0 !~ /^#/ { print }
' "$policy_file" > "$normalized_policy"

LC_ALL=C sort -u "$normalized_policy" > "$sorted_policy"
if ! cmp -s "$normalized_policy" "$sorted_policy"; then
  echo "identity policy entries must be sorted and unique" >&2
  exit 1
fi

line_number=0
set -f
while IFS= read -r policy_line; do
  line_number=$((line_number + 1))
  # Policy grammar is whitespace-delimited; pathname expansion is disabled.
  # shellcheck disable=SC2086
  set -- $policy_line
  directive=${1:-}
  case "$directive" in
    email)
      if [ "$#" -ne 2 ]; then
        echo "malformed identity policy directive at normalized line $line_number" >&2
        exit 1
      fi
      ;;
    domain|domain-tree)
      if [ "$#" -ne 2 ]; then
        echo "malformed identity policy directive at normalized line $line_number" >&2
        exit 1
      fi
      case "$2" in
        *[!a-z0-9.-]*|.*|*..*|*.)
          echo "malformed identity policy directive at normalized line $line_number" >&2
          exit 1
          ;;
      esac
      ;;
    grandfather)
      if [ "$#" -ne 4 ] || [ "${#2}" -ne 40 ]; then
        echo "malformed identity policy directive at normalized line $line_number" >&2
        exit 1
      fi
      case "$2" in
        *[!0-9a-f]*)
          echo "malformed identity policy directive at normalized line $line_number" >&2
          exit 1
          ;;
      esac
      case "$3" in
        author|committer|tagger|trailer) ;;
        *)
          echo "malformed identity policy directive at normalized line $line_number" >&2
          exit 1
          ;;
      esac
      ;;
    *)
      echo "unknown identity policy directive at normalized line $line_number" >&2
      exit 1
      ;;
  esac

  policy_value=${2:-}
  lower_value=$(printf '%s' "$policy_value" | tr '[:upper:]' '[:lower:]')
  if [ "$policy_value" != "$lower_value" ]; then
    echo "identity policy entries must be normalized at normalized line $line_number" >&2
    exit 1
  fi
  if [ "$directive" = "email" ] || [ "$directive" = "grandfather" ]; then
    email_value=${2:-}
    [ "$directive" = "grandfather" ] && email_value=${4:-}
    lower_email=$(printf '%s' "$email_value" | tr '[:upper:]' '[:lower:]')
    case "$email_value" in
      ''|*[[:space:]\<\>]*|*@|@*)
        echo "malformed identity policy directive at normalized line $line_number" >&2
        exit 1
        ;;
    esac
    if [ "$email_value" != "$lower_email" ]; then
      echo "identity policy entries must be normalized at normalized line $line_number" >&2
      exit 1
    fi
  fi
done < "$normalized_policy"
set +f

failure_count=0
checked_identity_count=0

check_identity() {
  object_id=$1
  field=$2
  raw_email=$3
  normalized_email=$(printf '%s' "$raw_email" \
    | sed 's/^[[:space:]]*//; s/[[:space:]]*$//' \
    | tr '[:upper:]' '[:lower:]')
  printf '%s\n' "$normalized_email" >> "$observed_email_file"

  case "$normalized_email" in
    ''|*@|@*|*[[:space:]\<\>]*)
      echo "identity policy rejected object $object_id field $field" >&2
      failure_count=$((failure_count + 1))
      return
      ;;
  esac

  email_domain=${normalized_email##*@}
  if awk -v email="$normalized_email" -v domain="$email_domain" -v oid="$object_id" -v field="$field" '
    $1 == "email" && $2 == email { allowed = 1 }
    $1 == "domain" && $2 == domain { allowed = 1 }
    $1 == "domain-tree" && (domain == $2 || (length(domain) > length($2) && substr(domain, length(domain) - length($2), length($2) + 1) == "." $2)) { allowed = 1 }
    $1 == "grandfather" && $2 == oid && $3 == field && $4 == email { allowed = 1 }
    END { exit allowed ? 0 : 1 }
  ' "$normalized_policy"; then
    checked_identity_count=$((checked_identity_count + 1))
  else
    echo "identity policy rejected object $object_id field $field" >&2
    failure_count=$((failure_count + 1))
  fi
}

validate_annotated_tag() {
  tag_oid=$1
  tagger_email=$(git cat-file -p "$tag_oid" \
    | sed -n 's/^tagger .*<\([^<>]*\)> [0-9][0-9]* [+-][0-9][0-9][0-9][0-9]$/\1/p')
  if [ -z "$tagger_email" ]; then
    echo "identity policy rejected object $tag_oid field tagger" >&2
    failure_count=$((failure_count + 1))
    return
  fi
  check_identity "$tag_oid" tagger "$tagger_email"
}

queue_rev() {
  revision=$1
  if ! object_type=$(git cat-file -t "$revision" 2>/dev/null); then
    echo "identity policy could not resolve object $revision" >&2
    failure_count=$((failure_count + 1))
    return
  fi

  case "$object_type" in
    commit)
      git rev-list "$revision" >> "$commit_file"
      ;;
    tag)
      validate_annotated_tag "$revision"
      if ! git rev-parse --verify "$revision^{commit}" >/dev/null 2>&1; then
        echo "identity policy rejected object $revision field target" >&2
        failure_count=$((failure_count + 1))
        return
      fi
      git rev-list "$revision^{commit}" >> "$commit_file"
      ;;
    *)
      echo "identity policy rejected object $revision field type" >&2
      failure_count=$((failure_count + 1))
      ;;
  esac
}

if [ "${#active_emails[@]}" -gt 0 ]; then
  for active_email in "${active_emails[@]}"; do
    check_identity local-config configured "$active_email"
  done
fi

if [ "${#ranges[@]}" -gt 0 ]; then
  for revision_range in "${ranges[@]}"; do
    if ! git rev-list "$revision_range" >> "$commit_file"; then
      echo "identity policy could not resolve the requested commit range" >&2
      failure_count=$((failure_count + 1))
    fi
  done
fi

if [ "${#revs[@]}" -gt 0 ]; then
  for revision in "${revs[@]}"; do
    queue_rev "$revision"
  done
fi

if [ "$scan_all" = true ]; then
  git for-each-ref \
    '--format=%(refname)%09%(objectname)%09%(objecttype)' \
    refs/heads refs/remotes refs/tags > "$ref_file"
  while IFS=$'\t' read -r ref_name object_id object_type; do
    [ "$ref_name" = "refs/remotes/origin/HEAD" ] && continue
    case "$object_type" in
      commit)
        git rev-list "$object_id" >> "$commit_file"
        ;;
      tag)
        validate_annotated_tag "$object_id"
        git rev-list "$object_id^{commit}" >> "$commit_file"
        ;;
      *)
        echo "identity policy rejected object $object_id field type" >&2
        failure_count=$((failure_count + 1))
        ;;
    esac
  done < "$ref_file"
fi

LC_ALL=C sort -u "$commit_file" > "$unique_commit_file"
checked_commit_count=0
while IFS= read -r commit_oid; do
  [ -n "$commit_oid" ] || continue
  if ! git cat-file -e "$commit_oid^{commit}" 2>/dev/null; then
    echo "identity policy could not resolve a queued commit" >&2
    failure_count=$((failure_count + 1))
    continue
  fi

  author_email=$(git show -s --format=%ae "$commit_oid")
  committer_email=$(git show -s --format=%ce "$commit_oid")
  check_identity "$commit_oid" author "$author_email"
  check_identity "$commit_oid" committer "$committer_email"

  : > "$trailer_file"
  if ! git show -s --format=%B "$commit_oid" \
    | git interpret-trailers --parse \
    | grep -Eo '<[^<>[:space:]]+@[^<>[:space:]]+>' \
    | tr -d '<>' > "$trailer_file"; then
    :
  fi
  while IFS= read -r trailer_email; do
    [ -n "$trailer_email" ] || continue
    check_identity "$commit_oid" trailer "$trailer_email"
  done < "$trailer_file"

  checked_commit_count=$((checked_commit_count + 1))
done < "$unique_commit_file"

if [ -n "$inventory_file" ]; then
  if [ "$(sed -n '1p' "$inventory_file")" != $'email\tnames\tsurfaces\tfirst_seen_oid\tlast_seen_oid\tdisposition' ]; then
    echo "identity inventory header is malformed" >&2
    failure_count=$((failure_count + 1))
  else
    sed '1d' "$inventory_file" | cut -f1 | LC_ALL=C sort -u > "$inventory_email_file"
    LC_ALL=C sort -u "$observed_email_file" > "$observed_email_file.sorted"
    if ! cmp -s "$inventory_email_file" "$observed_email_file.sorted"; then
      echo "identity inventory does not cover the scanned graph" >&2
      failure_count=$((failure_count + 1))
    fi
  fi
fi

if [ "$failure_count" -ne 0 ]; then
  echo "identity policy failed with $failure_count rejected or incomplete checks" >&2
  exit 1
fi

echo "identity policy passed: $checked_commit_count commits, $checked_identity_count identity fields"
