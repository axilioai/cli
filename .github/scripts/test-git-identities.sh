#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
checker="$repo_root/.github/scripts/check-git-identities.sh"
hook="$repo_root/.githooks/pre-push"

test_tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/git-identity-tests.XXXXXX")
trap 'rm -rf "$test_tmp_dir"' EXIT

fixture_repo="$test_tmp_dir/repo"
mkdir -p "$fixture_repo/.github/scripts" "$fixture_repo/.githooks"
git -C "$fixture_repo" init -q -b main
cp "$checker" "$fixture_repo/.github/scripts/check-git-identities.sh"
cp "$hook" "$fixture_repo/.githooks/pre-push"
chmod +x "$fixture_repo/.github/scripts/check-git-identities.sh" "$fixture_repo/.githooks/pre-push"

policy="$fixture_repo/policy.txt"
printf 'domain axilio.ai\ndomain-tree github.com\n' > "$policy"

git -C "$fixture_repo" config user.name "Identity Test"
git -C "$fixture_repo" config user.email "tester@axilio.ai"

make_commit() {
  author_email=$1
  committer_email=$2
  message=$3
  printf '%s\n' "$message" > "$fixture_repo/fixture.txt"
  git -C "$fixture_repo" add fixture.txt
  GIT_AUTHOR_NAME="Fixture Author" \
  GIT_AUTHOR_EMAIL="$author_email" \
  GIT_COMMITTER_NAME="Fixture Committer" \
  GIT_COMMITTER_EMAIL="$committer_email" \
    git -C "$fixture_repo" commit -q -m "$message"
  git -C "$fixture_repo" rev-parse HEAD
}

expect_failure() {
  label=$1
  denied_value=$2
  shift 2
  failure_log="$test_tmp_dir/failure.log"
  if "$@" > "$failure_log" 2>&1; then
    echo "expected failure: $label" >&2
    exit 1
  fi
  if [ -n "$denied_value" ] && grep -Fqi "$denied_value" "$failure_log"; then
    echo "failure output leaked a rejected identity: $label" >&2
    exit 1
  fi
}

run_checker() {
  target_repo=$1
  shift
  (
    cd "$target_repo"
    .github/scripts/check-git-identities.sh "$@"
  )
}

base_oid=$(make_commit "tester@axilio.ai" "tester@axilio.ai" "allowed baseline")
run_checker "$fixture_repo" --policy "$policy" --rev "$base_oid" >/dev/null

run_checker "$fixture_repo" --policy "$policy" --active-email "user@axilio.ai" >/dev/null
run_checker "$fixture_repo" --policy "$policy" --active-email "user@github.com" >/dev/null
run_checker "$fixture_repo" --policy "$policy" --active-email "user@users.noreply.github.com" >/dev/null
run_checker "$fixture_repo" --policy "$policy" --active-email "USER@AXILIO.AI" >/dev/null
run_checker "$fixture_repo" --policy "$policy" --active-email "local@part@github.com" >/dev/null
expect_failure "Axilio subdomain boundary" "user@sub.axilio.ai" \
  run_checker "$fixture_repo" --policy "$policy" --active-email "user@sub.axilio.ai"
expect_failure "GitHub lookalike boundary" "user@notgithub.com" \
  run_checker "$fixture_repo" --policy "$policy" --active-email "user@notgithub.com"
expect_failure "GitHub suffix boundary" "user@github.com.example.invalid" \
  run_checker "$fixture_repo" --policy "$policy" --active-email "user@github.com.example.invalid"

git -C "$fixture_repo" switch -q -c rejected-author "$base_oid"
rejected_author_oid=$(make_commit "unapproved@example.invalid" "tester@axilio.ai" "rejected author")
expect_failure "author" "unapproved@example.invalid" \
  run_checker "$fixture_repo" --policy "$policy" --rev "$rejected_author_oid"

git -C "$fixture_repo" switch -q -c rejected-committer "$base_oid"
rejected_committer_oid=$(make_commit "tester@axilio.ai" "unapproved@example.invalid" "rejected committer")
expect_failure "committer" "unapproved@example.invalid" \
  run_checker "$fixture_repo" --policy "$policy" --rev "$rejected_committer_oid"

git -C "$fixture_repo" switch -q -c rejected-trailer "$base_oid"
rejected_trailer_oid=$(make_commit "tester@axilio.ai" "tester@axilio.ai" $'rejected trailer\n\nCo-Authored-By: Fixture <unapproved@example.invalid>')
expect_failure "trailer" "unapproved@example.invalid" \
  run_checker "$fixture_repo" --policy "$policy" --rev "$rejected_trailer_oid"

git -C "$fixture_repo" switch -q main
GIT_COMMITTER_NAME="Fixture Tagger" \
GIT_COMMITTER_EMAIL="unapproved@example.invalid" \
  git -C "$fixture_repo" tag -a rejected-tagger -m "rejected tagger" "$base_oid"
expect_failure "tagger" "unapproved@example.invalid" \
  run_checker "$fixture_repo" --policy "$policy" --rev rejected-tagger

git -C "$fixture_repo" switch -q -c grandfather "$base_oid"
grandfather_oid=$(make_commit "tester@axilio.ai" "tester@axilio.ai" $'historical trailer\n\nCo-Authored-By: Historical Fixture <legacy@example.invalid>')
grandfather_policy="$fixture_repo/grandfather-policy.txt"
printf 'domain axilio.ai\ndomain-tree github.com\ngrandfather %s trailer legacy@example.invalid\n' "$grandfather_oid" > "$grandfather_policy"
run_checker "$fixture_repo" --policy "$grandfather_policy" --rev "$grandfather_oid" >/dev/null
new_occurrence_oid=$(make_commit "tester@axilio.ai" "tester@axilio.ai" $'new trailer occurrence\n\nCo-Authored-By: New Fixture <legacy@example.invalid>')
expect_failure "object-bound grandfather" "legacy@example.invalid" \
  run_checker "$fixture_repo" --policy "$grandfather_policy" --rev "$new_occurrence_oid"

unsorted_policy="$fixture_repo/unsorted-policy.txt"
printf 'domain-tree github.com\ndomain axilio.ai\n' > "$unsorted_policy"
expect_failure "unsorted policy" "" \
  run_checker "$fixture_repo" --policy "$unsorted_policy" --rev "$base_oid"

git -C "$fixture_repo" switch -q main
mkdir -p "$fixture_repo/.github/scripts" "$fixture_repo/.githooks"
cp "$checker" "$fixture_repo/.github/scripts/check-git-identities.sh"
cp "$hook" "$fixture_repo/.githooks/pre-push"
cp "$policy" "$fixture_repo/.github/git-identity-allowlist.txt"
git -C "$fixture_repo" add .github .githooks
make_commit "tester@axilio.ai" "tester@axilio.ai" "add identity policy fixtures" >/dev/null

remote_repo="$test_tmp_dir/remote.git"
git init -q --bare "$remote_repo"
git -C "$fixture_repo" remote add origin "$remote_repo"
git -C "$fixture_repo" push -q --no-verify -u origin main
git -C "$remote_repo" symbolic-ref HEAD refs/heads/main

hook_clone="$test_tmp_dir/hook-clone"
git clone -q "$remote_repo" "$hook_clone"
git -C "$hook_clone" config user.name "Identity Test"
git -C "$hook_clone" config user.email "tester@axilio.ai"
git -C "$hook_clone" config core.hooksPath .githooks
git -C "$hook_clone" config axilio.identityPolicyOrigin "$remote_repo"

git -C "$hook_clone" switch -q -c allowed-push
printf 'allowed\n' > "$hook_clone/allowed.txt"
git -C "$hook_clone" add allowed.txt
git -C "$hook_clone" commit -q -m "allowed push"

git -C "$hook_clone" switch -q -c rejected-push main
printf 'rejected\n' > "$hook_clone/rejected.txt"
git -C "$hook_clone" add rejected.txt
GIT_AUTHOR_NAME="Fixture Author" \
GIT_AUTHOR_EMAIL="unapproved@example.invalid" \
GIT_COMMITTER_NAME="Fixture Committer" \
GIT_COMMITTER_EMAIL="tester@axilio.ai" \
  git -C "$hook_clone" commit -q -m "rejected push"

printf 'domain axilio.ai\ndomain-tree example.invalid\ndomain-tree github.com\n' \
  > "$hook_clone/.github/git-identity-allowlist.txt"

receive_marker="$test_tmp_dir/remote-received"
cat > "$remote_repo/hooks/pre-receive" <<HOOK
#!/usr/bin/env bash
touch "$receive_marker"
HOOK
chmod +x "$remote_repo/hooks/pre-receive"

expect_failure "multi-ref pre-push" "unapproved@example.invalid" \
  git -C "$hook_clone" push origin allowed-push rejected-push
if [ -e "$receive_marker" ]; then
  echo "remote receive hook ran for a locally rejected multi-ref push" >&2
  exit 1
fi
if git -C "$remote_repo" show-ref --verify --quiet refs/heads/allowed-push || \
   git -C "$remote_repo" show-ref --verify --quiet refs/heads/rejected-push; then
  echo "a locally rejected multi-ref push changed a remote ref" >&2
  exit 1
fi

other_remote="$test_tmp_dir/other.git"
git init -q --bare "$other_remote"
git -C "$hook_clone" remote add other "$other_remote"
other_marker="$test_tmp_dir/other-received"
cat > "$other_remote/hooks/pre-receive" <<HOOK
#!/usr/bin/env bash
touch "$other_marker"
HOOK
chmod +x "$other_remote/hooks/pre-receive"
expect_failure "alternate destination" "unapproved@example.invalid" \
  git -C "$hook_clone" push other rejected-push
if [ -e "$other_marker" ]; then
  echo "alternate remote received a locally rejected object" >&2
  exit 1
fi

git -C "$hook_clone" push -q --no-verify origin allowed-push
git -C "$hook_clone" switch -q main
git -C "$hook_clone" push -q origin :allowed-push

shallow_clone="$test_tmp_dir/shallow"
git clone -q --depth 1 "file://$remote_repo" "$shallow_clone"
expect_failure "shallow repository" "" \
  run_checker "$shallow_clone" \
  --policy "$shallow_clone/.github/git-identity-allowlist.txt" \
  --rev HEAD

echo "identity policy fixtures passed"
