#!/usr/bin/env bash
# Start the in-container hardware crypto emulators for the devcontainer:
#   * swtpm  — TPM 2.0 simulator on TCP localhost:2321 (ctrl 2322), matching the
#              CMD in test/integration/docker/swtpm/Dockerfile.
#   * SoftHSM2 — initialise a default PKCS#11 token (idempotent).
#
# This is wired into devcontainer.json postStartCommand so the tpm2 / pkcs11
# build-tagged tests can run IN the devcontainer without the seal-hardware
# docker-compose stack. Integration tests that need a pristine token still
# re-init their own slot via softhsm2-util.
#
# The script is intentionally non-fatal: a failure here must not block the
# devcontainer from starting (e.g. when running on a host without the emulators
# built in). Each step logs and continues.
set -u

SWTPM_STATE_DIR="${SWTPM_STATE_DIR:-/var/lib/swtpm/tpmstate}"
SWTPM_PORT="${TPM_SIMULATOR_PORT:-2321}"
SWTPM_CTRL_PORT="$((SWTPM_PORT + 1))"
SWTPM_LOG="${SWTPM_LOG:-/tmp/swtpm.log}"
SOFTHSM_TOKEN_LABEL="${SOFTHSM_TOKEN_LABEL:-quicbaas-dev}"
SOFTHSM_PIN="${SOFTHSM_PIN:-1234}"
SOFTHSM_SO_PIN="${SOFTHSM_SO_PIN:-1234}"

log() { echo "[init-hardware] $*"; }

# --------------------------------------------------------------------------- #
# 1. swtpm — start the TPM 2.0 simulator on a TCP socket if not already up.
# --------------------------------------------------------------------------- #
if ! command -v swtpm >/dev/null 2>&1; then
    log "swtpm not installed; skipping TPM simulator (rebuild the devcontainer to enable tpm2 tests)"
elif nc -z localhost "${SWTPM_PORT}" 2>/dev/null; then
    log "swtpm already listening on localhost:${SWTPM_PORT}"
else
    mkdir -p "${SWTPM_STATE_DIR}" 2>/dev/null || sudo mkdir -p "${SWTPM_STATE_DIR}"
    log "starting swtpm on TCP localhost:${SWTPM_PORT} (ctrl ${SWTPM_CTRL_PORT})"
    nohup swtpm socket \
        --tpmstate "dir=${SWTPM_STATE_DIR}" \
        --tpm2 \
        --server "type=tcp,port=${SWTPM_PORT},bindaddr=127.0.0.1" \
        --ctrl "type=tcp,port=${SWTPM_CTRL_PORT},bindaddr=127.0.0.1" \
        --flags startup-clear,not-need-init \
        --log "level=20" \
        >"${SWTPM_LOG}" 2>&1 &
    for _ in $(seq 1 20); do
        if nc -z localhost "${SWTPM_PORT}" 2>/dev/null; then
            log "swtpm ready on localhost:${SWTPM_PORT}"
            break
        fi
        sleep 0.25
    done
    if ! nc -z localhost "${SWTPM_PORT}" 2>/dev/null; then
        log "WARNING: swtpm did not become ready; see ${SWTPM_LOG}"
    fi
fi

# --------------------------------------------------------------------------- #
# 2. SoftHSM2 — initialise a default token if none exists (idempotent).
# --------------------------------------------------------------------------- #
if ! command -v softhsm2-util >/dev/null 2>&1; then
    log "softhsm2-util not installed; skipping PKCS#11 token init (rebuild the devcontainer to enable pkcs11 tests)"
elif softhsm2-util --show-slots 2>/dev/null | grep -q "Label:.*${SOFTHSM_TOKEN_LABEL}"; then
    log "SoftHSM2 token '${SOFTHSM_TOKEN_LABEL}' already initialised"
else
    log "initialising SoftHSM2 token '${SOFTHSM_TOKEN_LABEL}'"
    softhsm2-util --init-token --free \
        --label "${SOFTHSM_TOKEN_LABEL}" \
        --so-pin "${SOFTHSM_SO_PIN}" \
        --pin "${SOFTHSM_PIN}" \
        || log "WARNING: SoftHSM2 token init failed (a pre-existing token may occupy slot 0)"
fi

log "hardware emulator init complete"
exit 0
