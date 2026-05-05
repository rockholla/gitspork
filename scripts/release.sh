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

remote_tags="$(git ls-remote --tags --sort=-v:refname origin 'refs/tags/v*' \
  | grep -v '\^{}' \
  | sed 's|.*refs/tags/||')"
latest_stable_tag="$(echo "$remote_tags" | grep -v -- '-' | head -1)"
latest_prerelease_tag="$(echo "$remote_tags" | grep -- '-' | head -1)"
if [ -n "$latest_stable_tag" ]; then
  info "Most recent stable tag: ${latest_stable_tag}"
else
  info "No stable remote tags found yet"
fi
if [ -n "$latest_prerelease_tag" ]; then
  info "Most recent pre-release tag: ${latest_prerelease_tag}"
fi

while true; do
  version="$(get_user_input "What version do you want to release?")"
  if [[ "$version" =~ $semver_regex ]]; then
    break
  fi
  warn "Please enter a valid semver with v prefix (e.g. v1.2.3)"
done

current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$current_branch" != "main" && ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+- ]]; then
  err "Stable releases require the main branch. Current branch: ${current_branch}. Use a pre-release version (e.g. ${version}-rc.1) or switch to main."
  handle_errors "exit 1"
fi

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
