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
DEFAULT_TAG="bmad-v6.12.4-r3"
DEFAULT_TARBALL="bmad-6.12.4-r3-arcaven.tar.gz"
DEFAULT_SHA="7b73b2bb22f736152e42119cb8c77252a78c65b09720aebcc6d888c8b5735465"

# run_script <release-dir> [VAR=value ...]: sets RC, OUT and SUMMARY (the step summary file).
run_script() {
    local rel="$1"
    shift
    SUMMARY="${WORK}/summary.md"
    : > "${SUMMARY}"
    set +e
    OUT="$(env -i PATH="${STUBS}:/usr/bin:/bin" GITHUB_STEP_SUMMARY="${SUMMARY}" "$@" bash "${SCRIPT}" "${rel}" /bin/true "${WORK}/scratch" 2>&1)"
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
run_script "${empty}" LIVE_VERIFY_TARBALL="${CUSTOM_TARBALL}" LIVE_VERIFY_SHA="${CUSTOM_SHA}"
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

# first_line_has <needle>: the script's first output line, before any check.
first_line_has() { head -1 <<< "${OUT}" | grep -Fq -- "$1"; }

# 2b. G1: with no LIVE_VERIFY_* set, the r3 values are the fallbacks, and a
#     placeholder in the release does not supply the sha. The tag is visible in
#     the first output line and in the summary heading, written before the checks.
r3="$(release_dir r3 "${DEFAULT_TARBALL}")"
run_script "${r3}"
if [[ "${RC}" -ne 0 ]] && grep -Fq "is not ${DEFAULT_SHA}" <<< "${OUT}" \
    && first_line_has "tag=${DEFAULT_TAG}" && grep -Fq "### Live Verify of ${DEFAULT_TAG}" "${SUMMARY}"; then
    pass "G1: the r3 tarball and sha are the fallbacks, and the tag shows in the first line and the summary heading"
else
    fail "G1 fallbacks" "rc=${RC}, summary: $(cat "${SUMMARY}"), output: ${OUT}"
fi

# 2c. G2, a positive control: a valid custom tag, pack, tarball and sha raise no
#     "invalid" line, reach the sha check and show the custom tag.
run_script "${d}" LIVE_VERIFY_TAG="${CUSTOM_TAG}" LIVE_VERIFY_PACK="${CUSTOM_PACK}" \
    LIVE_VERIFY_TARBALL="${CUSTOM_TARBALL}" LIVE_VERIFY_SHA="${CUSTOM_SHA}"
if [[ "${RC}" -ne 0 ]] && ! grep -Fq "invalid" <<< "${OUT}" && grep -Fq "is not ${CUSTOM_SHA}" <<< "${OUT}" \
    && first_line_has "tag=${CUSTOM_TAG}" && grep -Fq "### Live Verify of ${CUSTOM_TAG}" "${SUMMARY}"; then
    pass "G2: valid custom inputs are accepted and the custom tag shows in the first line and the summary heading"
else
    fail "G2 custom inputs" "rc=${RC}, summary: $(cat "${SUMMARY}"), output: ${OUT}"
fi

# 2d. G3: the job name shows the tag.
WORKFLOW="${HERE}/../.github/workflows/live-verify.yml"
if grep -Fq "name: Live Verify of \${{ inputs.tag || '${DEFAULT_TAG}' }}" "${WORKFLOW}"; then
    pass "G3: the job name shows the tag"
else
    fail "G3 job name" "no 'name: Live Verify of \${{ inputs.tag || ...}}' in ${WORKFLOW}"
fi

# 2e. G4: a custom tag, pack or tarball with no sha is refused. The sha is never
#     taken from r3 and never read from the release.
for v in "LIVE_VERIFY_TAG=${CUSTOM_TAG}" "LIVE_VERIFY_PACK=${CUSTOM_PACK}" "LIVE_VERIFY_TARBALL=${CUSTOM_TARBALL}"; do
    run_script "${d}" "${v}"
    if [[ "${RC}" -ne 0 ]] && grep -Fq "invalid LIVE_VERIFY_SHA" <<< "${OUT}"; then
        pass "G4: ${v%%=*} without LIVE_VERIFY_SHA is refused"
    else
        fail "G4 ${v%%=*} without a sha" "rc=${RC}, output: ${OUT}"
    fi
done

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
