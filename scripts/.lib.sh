#!/usr/bin/env bash

export NO_COLOR="\033[0m"
export ERROR_COLOR="\033[31m"
export WARN_COLOR="\033[33m"
export OK_COLOR="\033[32m"
export CYAN_COLOR="\033[36m"
export BOLD="\033[1m"

function require_variable_value() {
  local variable_name="$1"
  local variable_value="${!variable_name}"
  if [ -z "$variable_value" ]; then
    err "$variable_name is required"
  fi
}

function require_binary() {
  local name="$1"
  local install_hint="${2:-}"
  if ! command -v "$name" &>/dev/null; then
    if [ -n "$install_hint" ]; then
      err "'$name' is required but not found. Install it with: $install_hint"
    else
      err "'$name' is required but not found in PATH"
    fi
  fi
}

function warn() {
  local msg="$1"
  local warning="${WARN_COLOR}WARNING:${NO_COLOR} $msg"
  # shellcheck disable=SC2059
  >&2 printf "%b\n" "$warning"
}

_errors=false
function err() {
  local msg="$1"
  local err="${ERROR_COLOR}ERROR:${NO_COLOR} $msg"
  # shellcheck disable=SC2059
  >&2 printf "%b\n" "$err"
  _errors=true
}

function info() {
  local msg="$1"
  local info="${OK_COLOR}INFO:${NO_COLOR} $msg"
  # shellcheck disable=SC2059
  printf "%b\n" "$info"
}

function handle_errors() {
  local handler="$1"
  if [[ $_errors == true ]]; then
    eval "$handler"
  fi
}

# will prompt in a standard way for user input in the shell context
function get_user_input() {
  local prompt="$1"
  local user_input
  read -rep $'\001\033[38;2;255;166;0m\002➡️  '"${prompt}"$' \001\033[0m\002' user_input
  echo "$user_input"
}
