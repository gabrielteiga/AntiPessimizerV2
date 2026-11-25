#!/bin/sh
set -eu
root=$(git rev-parse --show-toplevel)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/owner-attribution.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM

git init -q "$fixture/repository"
git -C "$fixture/repository" config user.name 'Gabriel Teiga'
git -C "$fixture/repository" config user.email '116650677+gabrielteiga@users.noreply.github.com'
git -C "$fixture/repository" config user.useConfigOnly true
git -C "$fixture/repository" config core.hooksPath "$root/.githooks"
printf '%s\n' 'Attribution fixture' > "$fixture/repository/content.txt"
git -C "$fixture/repository" add content.txt

if GIT_AUTHOR_NAME='Other Author' GIT_AUTHOR_EMAIL='other@example.invalid' \
    git -C "$fixture/repository" commit -qm 'Check author' 2> "$fixture/author-error"; then
    printf '%s\n' 'An unapproved author was accepted.' >&2
    exit 1
fi
if GIT_COMMITTER_NAME='Other Committer' GIT_COMMITTER_EMAIL='other@example.invalid' \
    git -C "$fixture/repository" commit -qm 'Check committer' 2> "$fixture/committer-error"; then
    printf '%s\n' 'An unapproved committer was accepted.' >&2
    exit 1
fi
cat > "$fixture/message" <<'MESSAGE'
Check coauthor

Co-authored-by: Other Author <other@example.invalid>
MESSAGE
if git -C "$fixture/repository" commit -q -F "$fixture/message" 2> "$fixture/message-error"; then
    printf '%s\n' 'An automatic coauthor was accepted.' >&2
    exit 1
fi

git -C "$fixture/repository" commit -qm 'Verify owner attribution'
git init --bare -q "$fixture/remote"
git -C "$fixture/repository" remote add origin "$fixture/remote"
git -C "$fixture/repository" push -q origin HEAD:main

parent=$(git -C "$fixture/repository" rev-parse HEAD)
tree=$(git -C "$fixture/repository" rev-parse 'HEAD^{tree}')
rejected=$(printf '%s\n' 'Attribution rejection fixture' | \
    GIT_AUTHOR_NAME='Other Author' GIT_AUTHOR_EMAIL='other@example.invalid' \
    git -C "$fixture/repository" commit-tree "$tree" -p "$parent")
if git -C "$fixture/repository" push -q origin "$rejected:refs/heads/rejected" 2> "$fixture/push-error"; then
    printf '%s\n' 'Push accepted an unapproved unpublished commit.' >&2
    exit 1
fi
if git --git-dir="$fixture/remote" show-ref --verify --quiet refs/heads/rejected; then
    printf '%s\n' 'The rejected commit reached the remote.' >&2
    exit 1
fi
printf '%s\n' 'Owner attribution checks passed for author, committer, coauthor and push.'
