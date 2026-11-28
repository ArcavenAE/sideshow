#!/usr/bin/env bash
# Offline checks of how scripts/live-verify-r3.sh reads its inputs.
#
#   bash scripts/test-live-verify-inputs.sh
#
# Every run stops before the first cosign call: a stub cosign sits on PATH and
# the release directory holds placeholder files, so no network is touched.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${HERE}/live-verify-r3.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/live-verify-inputs.XXXXXX")"
STUBS="${WORK}/stubs"
mkdir -p "${STUBS}"
printf '#!/bin/sh\nexit 1\n' > "${STUBS}/cosign"
chmod +x "${STUBS}/cosign"

failures=0
pass() { echo "PASS $1"; }
fail() { echo "FAIL $1: $2"; failures=$((failures + 1)); }

CUSTOM_TAG="vsdd-factory-v1.0.0-rc.24"
CUSTOM_PACK="vsdd-factory"
CUSTOM_TARBALL="vsdd-factory-1.0.0-rc.24-arcaven.tar.gz"
CUSTOM_SHA="$(printf 'ab%.0s' {1..32})"

# run_script <release-dir> [VAR=value ...]: sets RC and OUT.
run_script() {
    local rel="$1"
    shift
    set +e
    OUT="$(env -i PATH="${STUBS}:/usr/bin:/bin" "$@" bash "${SCRIPT}" "${rel}" /bin/true "${WORK}/scratch" 2>&1)"
    RC=$?
    set -e
}

# release_dir <name> <tarball>: a directory holding the six placeholder assets.
release_dir() {
    local d="${WORK}/$1" t="$2"
    mkdir -p "${d}"
    for f in "${t}" "${t}.bundle" "${t}.attest.bundle" "${t}.attest.v2.bundle" file-manifest.csv exec-manifest.txt; do
        echo placeholder > "${d}/${f}"
    done
    echo "${d}"
}

# 1. The tarball name is an input: a release without it is reported by that name.
empty="${WORK}/empty"
mkdir -p "${empty}"
run_script "${empty}" LIVE_VERIFY_TARBALL="${CUSTOM_TARBALL}"
if [[ "${RC}" -ne 0 ]] && grep -Fq "missing ${empty}/${CUSTOM_TARBALL}" <<< "${OUT}"; then
    pass "the missing-asset check names the tarball from LIVE_VERIFY_TARBALL"
else
    fail "tarball input" "rc=${RC}, output: ${OUT}"
fi

# 2. The sha256 is the dispatcher's pin: the downloaded tarball is held to it.
d="$(release_dir pinned "${CUSTOM_TARBALL}")"
run_script "${d}" LIVE_VERIFY_TARBALL="${CUSTOM_TARBALL}" LIVE_VERIFY_SHA="${CUSTOM_SHA}"
if [[ "${RC}" -ne 0 ]] && grep -Fq "is not ${CUSTOM_SHA}" <<< "${OUT}"; then
    pass "the tarball is checked against LIVE_VERIFY_SHA"
else
    fail "sha input" "rc=${RC}, output: ${OUT}"
fi

# 3. Malformed inputs are refused before anything is downloaded or installed.
check_refused() { # label VAR=value
    local label="$1" var="${2%%=*}"
    shift
    run_script "${d}" LIVE_VERIFY_TARBALL="${CUSTOM_TARBALL}" LIVE_VERIFY_SHA="${CUSTOM_SHA}" "$@"
    if [[ "${RC}" -ne 0 ]] && grep -Fq "invalid ${var}" <<< "${OUT}"; then
        pass "refused: ${label}"
    else
        fail "refused: ${label}" "rc=${RC}, output: ${OUT}"
    fi
}
check_refused "a sha256 that is not 64 hex characters" LIVE_VERIFY_SHA=abc123
check_refused "a tag with a shell metacharacter" "LIVE_VERIFY_TAG=${CUSTOM_TAG};id"
check_refused "a tag that is not release-shaped" LIVE_VERIFY_TAG=main
check_refused "a pack with an upper-case letter" LIVE_VERIFY_PACK=Bmad
check_refused "a tarball with a path separator" LIVE_VERIFY_TARBALL=../x.tar.gz
check_refused "a tarball that is not a .tar.gz" LIVE_VERIFY_TARBALL=pack.zip

if [[ "${failures}" -ne 0 ]]; then
    echo "${failures} check(s) failed" >&2
    exit 1
fi
echo "all checks passed"
