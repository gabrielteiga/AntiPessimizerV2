#!/bin/sh
set -eu
: "${HEAD_SHA:?HEAD_SHA is required}"
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
branch=${DEFAULT_BRANCH:-main}
head=$(git rev-parse --verify "$HEAD_SHA^{commit}")
commits=$(mktemp "${TMPDIR:-${RUNNER_TEMP:-/tmp}}/owner-commits.XXXXXX")
trap 'rm -f "$commits"' EXIT HUP INT TERM

if test -n "${BASE_SHA:-}" && git cat-file -e "$BASE_SHA^{commit}" 2>/dev/null; then
    git rev-list "$head" "^$BASE_SHA" > "$commits"
elif test "${REF_NAME:-}" = "$branch"; then
    # A default-branch replacement must validate its complete submitted history.
    git rev-list "$head" > "$commits"
else
    # A force-updated topic branch may omit its previous tip from the checkout.
    git rev-list "$head" --not "refs/remotes/origin/$branch" > "$commits"
fi
while IFS= read -r commit; do
    "$root/.githooks/owner-policy" commit "$commit"
done < "$commits"
