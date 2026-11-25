#!/bin/sh
set -eu
root=$(git rev-parse --show-toplevel)
git config --local user.name 'Gabriel Teiga'
git config --local user.email '116650677+gabrielteiga@users.noreply.github.com'
git config --local user.useConfigOnly true
common=$(git rev-parse --path-format=absolute --git-common-dir)
hooks="$common/owner-hooks"
mkdir -p "$hooks"
cp "$root"/.githooks/* "$hooks/"
chmod +x "$hooks"/*
git config --local core.hooksPath "$hooks"
"$hooks/owner-policy" identity
