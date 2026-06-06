// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

// exec_init.go holds the low-level exec helpers shared between the
// matrix and the negative-path tests (e.g. PIN-rejection,
// unsupported-strategy). Kept separate from helpers.go because the
// negative-path callers need the raw subprocess output with the exit
// status preserved, while the matrix's runInit / runShamirInit wrappers
// hide the strategy-flag plumbing.

package init

import (
	"context"
	"os"
	"os/exec"
)

// execInit runs the quicbaasd binary with the supplied args and returns
// the combined stdout+stderr alongside the exec error. Caller is
// responsible for asserting whether the error indicates a clean exit
// (err == nil) or a failure (err != nil with exit code > 0).
//
// We deliberately propagate $PATH only so the daemon cannot pick up
// host-specific QUICBAAS_* env vars that would mask a regression.
func execInit(ctx context.Context, binary string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204 - args are produced by the test
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// hwTPMAvailable reports whether the operator opted into TPM2-backed
// barrier strategy testing by setting QUICBAAS_TEST_TPM2_SOCKET. The
// matrix gracefully skips hardware coverage when the var is absent (the
// only env-driven skip allowed per CLAUDE.md).
func hwTPMAvailable() bool {
	return os.Getenv("QUICBAAS_TEST_TPM2_SOCKET") != ""
}

// hwSoftHSMAvailable reports whether QUICBAAS_TEST_SOFTHSM2_MODULE points
// at a usable SoftHSM2 module path. Mirrors hwTPMAvailable: absent ->
// skip; present -> defer to a dedicated env-gated suite.
func hwSoftHSMAvailable() bool {
	return os.Getenv("QUICBAAS_TEST_SOFTHSM2_MODULE") != ""
}
