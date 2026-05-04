#!/usr/bin/env bash

set -eo pipefail

this_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
. "${this_dir}/.lib.sh"

version=""
description=""

semver_regex='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$'
semver_latest_regex='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

require_binary "goreleaser"
require_binary "git"

if [ -n "$(git status --porcelain)" ]; then
  err "Releasing only allowed on a clean working tree"
fi
handle_errors "exit 1"

while true; do
  version="$(get_user_input "What version do you want to release?")"
  if [[ "$version" =~ $semver_regex ]]; then
    break
  fi
  warn "Please enter a valid semver"
done

if [ -n "$(git tag -l "${version}")" ]; then
  err "git tag for version ${version} already exists locally"
fi
if git ls-remote --tags "$(git config --get remote.origin.url)" | grep -E '\trefs/tags/'"${version}"'$' &>/dev/null; then
  err "git tag for version ${version} already exists in remote origin repo"
fi
handle_errors "exit 1"

while true; do
  description="$(get_user_input "What's the description for this release?")"
  if [ -n "$description" ]; then
    break
  fi
  warn "Description can't be blank"
done

info "Releasing gitspork version: ${version}, tag description: ${description}"
confirm="$(get_user_input "Proceed? [y/N]")"
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
  info "Aborted."
  exit 0
fi

git tag -a "${version}" -m "${description}"
git push origin "${version}" || { git tag -d "${version}" 2>/dev/null || true; exit 1; }

latest=false
if [[ "$version" =~ $semver_latest_regex ]]; then
  latest=true
fi

GITSPORK_VERSION="${version}" IS_LATEST="${latest}" \
  goreleaser release --verbose --clean || { git tag -d "${version}" 2>/dev/null || true; git push origin --delete "${version}" 2>/dev/null || true; exit 1; }
