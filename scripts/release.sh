#!/usr/bin/env bash

set -eo pipefail

this_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
. "${this_dir}/.lib.sh"

version=""
description=""

semver_regex='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$'

require_binary "git"

if [ -n "$(git status --porcelain)" ]; then
  err "Releasing only allowed on a clean working tree"
fi
handle_errors "exit 1"

if [[ ! -t 0 ]]; then
  err "This script must be run interactively (stdin is not a terminal)"
  exit 1
fi

if ! git remote get-url origin &>/dev/null; then
  err "No 'origin' remote configured"
  exit 1
fi

latest_tag="$(git ls-remote --tags --sort=-v:refname origin 'refs/tags/v*' \
  | grep -v '\^{}' \
  | head -1 \
  | sed 's|.*refs/tags/||')"
if [ -n "$latest_tag" ]; then
  info "Most recent remote tag: ${latest_tag}"
else
  info "No remote tags found yet"
fi

while true; do
  version="$(get_user_input "What version do you want to release?")"
  if [[ "$version" =~ $semver_regex ]]; then
    break
  fi
  warn "Please enter a valid semver with v prefix (e.g. v1.2.3)"
done

if [ -n "$(git tag -l "${version}")" ]; then
  err "git tag for version ${version} already exists locally"
fi
if git ls-remote --exit-code --tags origin "${version}" &>/dev/null; then
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

repo_url="$(git remote get-url origin | sed 's|git@github.com:|https://github.com/|; s|\.git$||')"
info "Tag ${version} pushed. Watch the release workflow at: ${repo_url}/actions?query=branch%3A${version}"
