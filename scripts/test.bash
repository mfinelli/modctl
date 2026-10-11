#!/usr/bin/env bash
#
# Run the tests in a sandbox, and fail if they touched anything outside of it.
#
# The tests are supposed to be hermetic: they must not read or change the state
# of the machine that they run on (the XDG directories, the home directory, the
# temporary directory). Nothing in `go test` itself enforces that, so this does:
#
#   - HOME, every XDG_* directory and TMPDIR point into a temporary directory;
#   - the directories that the program would write its state to are read-only, so
#     that a test that tries to is a failing test, with a stack that says where
#     (this does not apply to root, which can write anywhere, so see below);
#   - afterwards, the sandbox must be empty, and so must the repository (no files
#     that were not there before).
#
# Usage: scripts/test.bash [go test arguments]
#
# Without arguments the tests of every package are run. The arguments are given
# to `go test`, after -race and -count=1 (the results of a test are never taken
# from the cache: a test that is not run can not find anything).

set -euo pipefail

if [[ $# -eq 0 ]]; then
  set -- ./...
fi

# Where Go keeps its files depends on HOME and XDG_CONFIG_HOME, which are about
# to change, so they are fixed first. The caches are shared with the rest of the
# machine, which is not what is being tested.
GOCACHE="$(go env GOCACHE)"
GOMODCACHE="$(go env GOMODCACHE)"
GOPATH="$(go env GOPATH)"
GOENV="$(go env GOENV)"
export GOCACHE GOMODCACHE GOPATH GOENV

sandbox="$(mktemp -d)"

cleanup() {
  # a read-only directory can't be removed until it is writable again
  chmod -R u+w "${sandbox}" 2> /dev/null || true
  rm -rf "${sandbox}"
}
trap cleanup EXIT

# the directories that must stay empty
guarded=(home config data state cache runtime)

for d in "${guarded[@]}" tmp; do
  mkdir -p "${sandbox}/${d}"
done

export HOME="${sandbox}/home"
export XDG_CONFIG_HOME="${sandbox}/config"
export XDG_DATA_HOME="${sandbox}/data"
export XDG_STATE_HOME="${sandbox}/state"
export XDG_CACHE_HOME="${sandbox}/cache"
export XDG_RUNTIME_DIR="${sandbox}/runtime"
export TMPDIR="${sandbox}/tmp"
unset XDG_DATA_DIRS XDG_CONFIG_DIRS

# TMPDIR has to stay writable (the tests make their directories there), so a
# leftover in it is only found afterwards.
for d in "${guarded[@]}"; do
  chmod a-w "${sandbox}/${d}"
done

repo_state() {
  if git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
    git status --porcelain --ignored
  fi
}

repo_before="$(repo_state)"

status=0
go test -race -count=1 "$@" || status=$?

leaks=0

# Go writes usage counters of its own to "go" in the config directory (when it
# can: it does not mind when it can't), which is not what is being looked for.
found="$(find "${sandbox}" -mindepth 2 \
  -not -path "${sandbox}/config/go" -not -path "${sandbox}/config/go/*" | sort)"
if [[ -n ${found} ]]; then
  echo >&2
  echo >&2 "The tests left files outside of their own directories:"
  # shown relative to the sandbox, which is where the XDG directories are
  echo >&2 "${found//${sandbox}\//  }"
  leaks=1
fi

repo_after="$(repo_state)"
if [[ ${repo_before} != "${repo_after}" ]]; then
  echo >&2
  echo >&2 "The tests changed the files of the repository:"
  diff <(echo "${repo_before}") <(echo "${repo_after}") >&2 || true
  leaks=1
fi

if [[ ${leaks} -ne 0 ]]; then
  exit 1
fi

exit "${status}"
