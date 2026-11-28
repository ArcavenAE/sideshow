#!/usr/bin/env bash
# Live check of signed-release install against a published release (aae-orc-hti53).
#
#   bash scripts/live-verify-r3.sh <release-dir> <sideshow-binary> <scratch-dir>
#
# <release-dir> holds the assets of bmad-v6.12.4-r3 (tarball, .bundle,
# .attest.bundle, .attest.v2.bundle, file-manifest.csv, exec-manifest.txt). The binary is the build under test and the
# real cosign must be on PATH. Four installs, each with its own HOME,
# SIDESHOW_HOME and TMPDIR under <scratch-dir>. Run in CI, where the Sigstore
# connects happen; do not run it on a fleet host.
set -euo pipefail

TAG="bmad-v6.12.4-r3"
PACK="bmad"
TARBALL="bmad-6.12.4-r3-arcaven.tar.gz"
SHA="7b73b2bb22f736152e42119cb8c77252a78c65b09720aebcc6d888c8b5735465"
WANT_SAN="https://github.com/ArcavenAE/sideshow-packs/.github/workflows/reattest-release.yml@refs/heads/main"

REL="${1:?release dir}"
BIN="${2:?sideshow binary}"
SCRATCH="${3:?scratch dir}"
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

failures=0
results=()
note() { echo "[live-verify] $*"; }
ok() { results+=("PASS $1"); note "PASS $1"; }
bad() { results+=("FAIL $1: $2"); note "FAIL $1: $2"; failures=$((failures + 1)); }

for f in "${TARBALL}" "${TARBALL}.bundle" "${TARBALL}.attest.bundle" "${TARBALL}.attest.v2.bundle" file-manifest.csv exec-manifest.txt; do
    [[ -f "${REL}/${f}" ]] || { echo "missing ${REL}/${f}" >&2; exit 1; }
done
command -v cosign > /dev/null || { echo "cosign is not on PATH" >&2; exit 1; }
actual="$(shasum -a 256 "${REL}/${TARBALL}" | cut -d' ' -f1)"
[[ "${actual}" == "${SHA}" ]] || { echo "downloaded tarball sha ${actual} is not ${SHA}" >&2; exit 1; }

# install <case> <release-dir> [extra args...]: sets RC, OUT_FILE, CASE_HOME.
install_case() {
    local name="$1" dir="$2"
    shift 2
    CASE_HOME="${SCRATCH}/${name}"
    mkdir -p "${CASE_HOME}/home" "${CASE_HOME}/sideshow" "${CASE_HOME}/tmp"
    OUT_FILE="${CASE_HOME}/out.txt"
    set +e
    env -i HOME="${CASE_HOME}/home" SIDESHOW_HOME="${CASE_HOME}/sideshow" TMPDIR="${CASE_HOME}/tmp" \
        PATH="${PATH}" "${BIN}" install "${PACK}" --from "${dir}" --yes --no-permissions "$@" > "${OUT_FILE}" 2>&1
    RC=$?
    set -e
    echo "----- ${name}: rc=${RC}"
    cat "${OUT_FILE}"
}

registry_has() { # home needle
    [[ -f "$1/sideshow/registry.yaml" ]] && grep -Fq "$2" "$1/sideshow/registry.yaml"
}

check_positive() { # case
    if [[ "${RC}" -ne 0 ]]; then bad "$1" "install exited ${RC}"; return; fi
    grep -Fq "Verified" "${OUT_FILE}" || { bad "$1" "no Verified line in the output"; return; }
    registry_has "${CASE_HOME}" "tarball_sha256: ${SHA}" || { bad "$1" "registry has no tarball_sha256 ${SHA}"; return; }
    registry_has "${CASE_HOME}" "verified: true" || { bad "$1" "registry has no verified: true"; return; }
    ok "$1"
}

# a. Positive, with the pin.
install_case a-positive "${REL}" --sha256 "${SHA}"
check_positive "a. install ${TAG} with --sha256 ${SHA:0:8}..."

# b. The v2 bundle is the one used: the older attest bundle is replaced by {},
#    which carries no provenance, so only the v2 bundle can satisfy Verify.
B="${SCRATCH}/rel-b"
mkdir -p "${B}"
cp "${REL}"/* "${B}/"
echo '{}' > "${B}/${TARBALL}.attest.bundle"
install_case b-v2-used "${B}" --sha256 "${SHA}"
check_positive "b. install with the older attest bundle replaced by {} (the v2 bundle is the one used)"

# c. Negative control: a zeroed pin is refused and the store stays empty.
install_case c-wrong-pin "${REL}" --sha256 "$(printf '0%.0s' {1..64})"
if [[ "${RC}" -eq 0 ]]; then
    bad "c. zeroed --sha256" "install succeeded"
elif ! grep -Fq "sha256 pin mismatch" "${OUT_FILE}"; then
    bad "c. zeroed --sha256" "refused, but not with the pin message"
elif [[ -e "${CASE_HOME}/sideshow/packs/${PACK}" ]] || registry_has "${CASE_HOME}" "name: ${PACK}"; then
    bad "c. zeroed --sha256" "the store holds ${PACK} after a refusal"
else
    ok "c. zeroed --sha256 refused with the pin message, store empty"
fi

# d. The v2 bundle left out: the older attest bundle was signed by build-pack.yml
#    at a tag, which the attestation pin (main only) does not accept, so the
#    install is refused at the cosign check.
D="${SCRATCH}/rel-d"
mkdir -p "${D}"
for f in "${REL}"/*; do
    [[ "${f}" == *.attest.v2.bundle ]] || cp "${f}" "${D}/"
done
install_case d-no-v2 "${D}" --sha256 "${SHA}"
if [[ "${RC}" -eq 0 ]]; then
    bad "d. v2 bundle removed" "install succeeded"
elif ! grep -Fq "none of the expected identities matched" "${OUT_FILE}"; then
    bad "d. v2 bundle removed" "refused, but not by the attestation identity pin"
else
    ok "d. v2 bundle removed: refused by the attestation identity pin (the older bundle is signed from a tag)"
fi

# The v2 SAN, read from the bundle's own certificate.
san="$(python3 -I - "${REL}/${TARBALL}.attest.v2.bundle" <<'PY' | openssl x509 -inform DER -noout -text | sed -n 's/^ *URI:\(.*\)$/\1/p' | head -1
import base64, json, sys
vm = json.load(open(sys.argv[1]))["verificationMaterial"]
c = vm.get("certificate", {}).get("rawBytes") or vm["x509CertificateChain"]["certificates"][0]["rawBytes"]
sys.stdout.buffer.write(base64.b64decode(c))
PY
)"
note "v2 SAN: ${san}"
if [[ "${san}" != "${WANT_SAN}" ]]; then
    bad "SAN" "the v2 bundle certificate URI is ${san}, want ${WANT_SAN}"
fi

{
    echo "### Live Verify of ${TAG}"
    echo
    echo "v2 SAN: ${san}"
    echo
    echo "tarball sha256: \`${SHA}\`"
    echo
    for r in "${results[@]}"; do echo "- ${r}"; done
} >> "${SUMMARY}"

[[ "${failures}" -eq 0 ]] || { echo "live verify: ${failures} check(s) failed" >&2; exit 1; }
note "all checks passed"
