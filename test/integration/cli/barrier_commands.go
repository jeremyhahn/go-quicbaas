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

// barrier_commands.go provides CommandDef builders for the canonical
// 9-verb barrier surface that ships in quicbaas / qrdbctl / xkmsctl /
// platformctl. The builders are deliberately transport-agnostic so the
// surrounding tests can drive them through RunForAllTransports.
//
// Mutation verbs (init, init-shamir, unseal, unseal-shares, seal, rekey,
// recovery) are not safe to run against the shared integration cluster
// because barrier state is daemon-global — one rogue seal stops every
// downstream subtest cold. Tests that exercise those verbs MUST spawn a
// fresh daemon via spawnFreshBarrierDaemon and route the CommandDef
// through a per-test TestRunner pointing at that daemon.
//
// The transport-skip set covers the canonical surface: every transport
// the SDK BarrierClient exposes is fair game. The CLI delegates through
// sdkClientFactory().Security().Barrier() which works over gRPC, Unix,
// QUIC, REST, MCP, and embedded. The matrix asserts each transport
// independently so a regression in any transport's barrier wiring (e.g.
// REST handler returns the wrong envelope) surfaces in one cell rather
// than failing silently behind a green test on the other transports.

package cli

import (
	"fmt"
	"strings"
	"time"
)

// barrierTransportsToSkip enumerates transports that have NOT yet wired
// the canonical 9-verb barrier surface end-to-end. Empty by design: the
// SDK BarrierClient is uniformly available across every transport, so
// every cell is exercised. If a transport regresses, narrow this slice
// rather than altering the per-verb skip list ad-hoc.
var barrierTransportsToSkip = []Transport{}

// =============================================================================
// Status verb
// =============================================================================

// BarrierStatusCommand builds `quicbaas barrier status`.
func BarrierStatusCommand() CommandDef {
	return CommandDef{
		Name:           "barrier status",
		Args:           []string{"barrier", "status"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierStatusJSONCommand builds `quicbaas barrier status --output json`.
func BarrierStatusJSONCommand() CommandDef {
	return CommandDef{
		Name:           "barrier status json",
		Args:           []string{"barrier", "status", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Init verb
// =============================================================================

// BarrierInitCommand builds `quicbaas barrier init` with the supplied
// strategy. secret is omitted under "none" so callers do not need to
// remember the per-strategy flag matrix.
func BarrierInitCommand(strategy, secret string) CommandDef {
	args := []string{"barrier", "init", "--barrier-strategy", strategy}
	if secret != "" {
		args = append(args, "--passphrase", secret)
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier init %s", strategy),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitWithPINsCommand builds `quicbaas barrier init` with the
// supplied strategy + secret + SO/User PIN pair. Used by the dual-control
// matrix row.
func BarrierInitWithPINsCommand(strategy, secret, soPin, userPin string) CommandDef {
	args := []string{
		"barrier", "init",
		"--barrier-strategy", strategy,
		"--passphrase", secret,
		"--so-pin", soPin,
		"--user-pin", userPin,
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier init %s (pinned)", strategy),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitNoneRejectsPINsCommand builds the negative-path command
// asserting that --so-pin/--user-pin are rejected under
// --barrier-strategy=none. The test asserts ExpectSuccess=false and that
// the PINsNotAllowedError string surfaces in stderr.
func BarrierInitNoneRejectsPINsCommand(soPin, userPin string) CommandDef {
	args := []string{
		"barrier", "init",
		"--barrier-strategy", "none",
		"--so-pin", soPin,
		"--user-pin", userPin,
	}
	return CommandDef{
		Name:           "barrier init none rejects pins",
		Args:           args,
		ExpectSuccess:  false,
		ExpectError:    []string{"--so-pin/--user-pin are not allowed", "barrier-strategy=none"},
		Timeout:        15 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitInvalidStrategyCommand builds the negative-path command
// asserting an unknown --barrier-strategy is rejected with the canonical
// validation error from cmd/quicbaas/barrier.go.
func BarrierInitInvalidStrategyCommand() CommandDef {
	args := []string{
		"barrier", "init",
		"--barrier-strategy", "bogus",
		"--passphrase", "x",
	}
	return CommandDef{
		Name:           "barrier init invalid strategy",
		Args:           args,
		ExpectSuccess:  false,
		ExpectError:    []string{"none", "passphrase", "shamir"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitMissingSecretCommand asserts the canonical "secret required
// for passphrase/shamir" rule.
func BarrierInitMissingSecretCommand(strategy string) CommandDef {
	args := []string{"barrier", "init", "--barrier-strategy", strategy}
	return CommandDef{
		Name:           fmt.Sprintf("barrier init %s missing secret", strategy),
		Args:           args,
		ExpectSuccess:  false,
		ExpectError:    []string{"--passphrase is required"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitSoloPinCommand asserts the dual-control rule: --so-pin
// without --user-pin (or vice versa) is rejected.
func BarrierInitSoloPinCommand(pinFlag, pinValue string) CommandDef {
	args := []string{
		"barrier", "init",
		"--barrier-strategy", "passphrase",
		"--passphrase", "pp",
		pinFlag, pinValue,
	}
	missing := "--user-pin"
	if pinFlag == "--user-pin" {
		missing = "--so-pin"
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier init solo %s requires %s", pinFlag, missing),
		Args:           args,
		ExpectSuccess:  false,
		ExpectError:    []string{"requires"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Init-Shamir verb
// =============================================================================

// BarrierInitShamirCommand builds `quicbaas barrier init-shamir` with the
// supplied secret + threshold + total.
func BarrierInitShamirCommand(secret string, threshold, total int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("barrier init-shamir %d-of-%d", threshold, total),
		Args: []string{
			"barrier", "init-shamir",
			"--passphrase", secret,
			"--threshold", fmt.Sprintf("%d", threshold),
			"--total", fmt.Sprintf("%d", total),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitShamirJSONCommand mirrors BarrierInitShamirCommand but
// requests JSON output so the test can decode the share list directly.
func BarrierInitShamirJSONCommand(secret string, threshold, total int) CommandDef {
	cmd := BarrierInitShamirCommand(secret, threshold, total)
	cmd.Args = append(cmd.Args, "--output", "json")
	cmd.Name = cmd.Name + " (json)"
	return cmd
}

// BarrierInitShamirMissingFlagCommand builds the required-flag negative
// matrix used by TestBarrierInit_ShamirRequiresParams. The omitFlag
// parameter selects which of (--passphrase, --threshold, --total) is removed.
func BarrierInitShamirMissingFlagCommand(omitFlag string) CommandDef {
	args := []string{"barrier", "init-shamir"}
	if omitFlag != "--passphrase" {
		args = append(args, "--passphrase", "test-secret")
	}
	if omitFlag != "--threshold" {
		args = append(args, "--threshold", "2")
	}
	if omitFlag != "--total" {
		args = append(args, "--total", "3")
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier init-shamir missing %s", omitFlag),
		Args:           args,
		ExpectSuccess:  false,
		ExpectError:    []string{strings.TrimPrefix(omitFlag, "--")},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierInitShamirThresholdExceedsTotalCommand asserts the canonical
// "threshold > total" validation error.
func BarrierInitShamirThresholdExceedsTotalCommand() CommandDef {
	return CommandDef{
		Name: "barrier init-shamir threshold > total",
		Args: []string{
			"barrier", "init-shamir",
			"--passphrase", "test",
			"--threshold", "5",
			"--total", "3",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"--threshold cannot exceed --total"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Unseal verb (passphrase)
// =============================================================================

// BarrierUnsealCommand builds `quicbaas barrier unseal --passphrase <pp>`.
func BarrierUnsealCommand(secret string) CommandDef {
	return CommandDef{
		Name:           "barrier unseal",
		Args:           []string{"barrier", "unseal", "--passphrase", secret},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierUnsealMissingSecretCommand asserts the canonical "--passphrase
// required" error from the unseal verb.
func BarrierUnsealMissingSecretCommand() CommandDef {
	return CommandDef{
		Name:           "barrier unseal missing secret",
		Args:           []string{"barrier", "unseal"},
		ExpectSuccess:  false,
		ExpectError:    []string{"--passphrase is required"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Unseal-Shares verb
// =============================================================================

// BarrierUnsealSharesFlagsCommand builds `barrier unseal-shares` with the
// supplied shares passed as repeatable --share flags.
func BarrierUnsealSharesFlagsCommand(shares []string) CommandDef {
	args := []string{"barrier", "unseal-shares"}
	for _, s := range shares {
		args = append(args, "--share", s)
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier unseal-shares (%d flags)", len(shares)),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierUnsealSharesFileCommand builds `barrier unseal-shares
// --shares-file <path>` reading the shares from a newline-delimited file.
func BarrierUnsealSharesFileCommand(path string) CommandDef {
	return CommandDef{
		Name:           "barrier unseal-shares (file)",
		Args:           []string{"barrier", "unseal-shares", "--shares-file", path},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierUnsealSharesStdinCommand builds `barrier unseal-shares
// --shares-file -` for piped stdin input. The runner is responsible for
// piping the shares into stdin via the Stdin field added below.
func BarrierUnsealSharesStdinCommand() CommandDef {
	return CommandDef{
		Name:           "barrier unseal-shares (stdin)",
		Args:           []string{"barrier", "unseal-shares", "--shares-file", "-"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierUnsealSharesBothInputsCommand asserts the mutual-exclusion rule
// (`--share` and `--shares-file` are not allowed together).
func BarrierUnsealSharesBothInputsCommand() CommandDef {
	return CommandDef{
		Name: "barrier unseal-shares both inputs",
		Args: []string{
			"barrier", "unseal-shares",
			"--share", "a",
			"--shares-file", "/tmp/x",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"exactly one of"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierUnsealSharesNoInputsCommand asserts the "at least one of"
// invariant.
func BarrierUnsealSharesNoInputsCommand() CommandDef {
	return CommandDef{
		Name:           "barrier unseal-shares no inputs",
		Args:           []string{"barrier", "unseal-shares"},
		ExpectSuccess:  false,
		ExpectError:    []string{"is required"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Seal verb
// =============================================================================

// Default SO/User PIN values used by the per-test daemon's init ceremony.
// The canonical init flow REQUIRES --so-pin/--user-pin for any
// barrier-enabled strategy (software/shamir); the no-barrier "none"
// strategy REJECTS them. These are throwaway test credentials: init
// provisions them, and the server verifies defaultBarrierSOPin before it
// seals on every transport, so every seal in the suite must send it.
// defaultBarrierUserPin is declared in barrier_test.go.
const defaultBarrierSOPin = "SO-PIN-TEST-0001"

// barrierSealSOPinEnv is the environment every network `barrier seal` in
// the suite runs with: the server verifies the SO PIN that init provisioned
// (defaultBarrierSOPin) before it seals, and the CLI reads it from
// QUICBAAS_BARRIER_SO_PIN, which keeps it off argv.
func barrierSealSOPinEnv() []string {
	return []string{"QUICBAAS_BARRIER_SO_PIN=" + defaultBarrierSOPin}
}

// BarrierSealAssumeYesCommand builds `barrier seal -y` so the interactive
// confirmation prompt is bypassed. The canonical scripted-runbook path. It
// carries the provisioned SO PIN (barrierSealSOPinEnv).
func BarrierSealAssumeYesCommand() CommandDef {
	return CommandDef{
		Name:           "barrier seal -y",
		Args:           []string{"barrier", "seal", "-y"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
		Env:            barrierSealSOPinEnv(),
	}
}

// BarrierSealCommand builds `barrier seal` without --assume-yes. The
// caller MUST pipe "yes\n" into stdin via runBarrierCommandWithStdin to
// satisfy the confirmation prompt. It carries the provisioned SO PIN
// (barrierSealSOPinEnv).
func BarrierSealCommand() CommandDef {
	return CommandDef{
		Name:           "barrier seal (interactive)",
		Args:           []string{"barrier", "seal"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
		Env:            barrierSealSOPinEnv(),
	}
}

// BarrierSealCancelCommand builds the same command as BarrierSealCommand
// but is invoked with "no\n" stdin so the cancel path is exercised.
func BarrierSealCancelCommand() CommandDef {
	return CommandDef{
		Name:           "barrier seal cancel",
		Args:           []string{"barrier", "seal"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Seal cancelled"},
		Timeout:        15 * time.Second,
		SkipTransports: barrierTransportsToSkip,
		Env:            barrierSealSOPinEnv(),
	}
}

// BarrierSealWithSOPinCommand builds `barrier seal --so-pin <pin> -y`.
// Used by the SO-required path.
func BarrierSealWithSOPinCommand(soPin string) CommandDef {
	return CommandDef{
		Name: "barrier seal --so-pin -y",
		Args: []string{
			"barrier", "seal",
			"--so-pin", soPin,
			"-y",
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierSealWithWrongSOPinCommand builds `barrier seal --so-pin <pin> -y`
// with a PIN the daemon did not provision: the server verifies the SO PIN
// before it seals and refuses this one, so the command must fail. The
// refusal text differs per transport (REST 403, gRPC Unauthenticated), so
// the caller proves the refusal by asserting the barrier is still unsealed.
func BarrierSealWithWrongSOPinCommand(soPin string) CommandDef {
	return CommandDef{
		Name: "barrier seal --so-pin <wrong> -y",
		Args: []string{
			"barrier", "seal",
			"--so-pin", soPin,
			"-y",
		},
		ExpectSuccess:  false,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Rekey verb
// =============================================================================

// BarrierRekeyCommand builds the canonical share-free rekey verb:
// `barrier rekey --so-pin <defaultBarrierSOPin> --threshold <M> --total <N>`.
// No secret travels on the request — the server generates a fresh unseal key
// and seals new shares to the enrolled Security Officers (qrdb
// pkg/seal/rekey.go). The per-test daemon provisions defaultBarrierSOPin at
// init and the server verifies the SO PIN on rekey as it does on seal, so the
// verb sends the provisioned PIN to reach the backend.
func BarrierRekeyCommand(threshold, total int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("barrier rekey --so-pin <pin> %d-of-%d", threshold, total),
		Args: []string{
			"barrier", "rekey",
			"--so-pin", defaultBarrierSOPin,
			"--threshold", fmt.Sprintf("%d", threshold),
			"--total", fmt.Sprintf("%d", total),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRekeyWithoutSOPinCommand builds the rekey verb with the same
// threshold/total pair as BarrierRekeyCommand but no `--so-pin`. The server
// refuses it at the SO-PIN check, before the request reaches the backend.
func BarrierRekeyWithoutSOPinCommand(threshold, total int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("barrier rekey (no SO PIN) %d-of-%d", threshold, total),
		Args: []string{
			"barrier", "rekey",
			"--threshold", fmt.Sprintf("%d", threshold),
			"--total", fmt.Sprintf("%d", total),
		},
		ExpectSuccess:  false,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRekeyUnpairedShamirCommand asserts that --threshold without
// --total (or vice versa) is rejected.
func BarrierRekeyUnpairedShamirCommand(includeFlag string) CommandDef {
	args := []string{"barrier", "rekey"}
	if includeFlag == "--threshold" {
		args = append(args, "--threshold", "3")
	} else {
		args = append(args, "--total", "5")
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier rekey unpaired %s", includeFlag),
		Args:           args,
		ExpectSuccess:  false,
		ExpectError:    []string{"must be supplied together"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRekeyBadThresholdCommand asserts the "threshold >= 2" rule on the
// rekey verb.
func BarrierRekeyBadThresholdCommand() CommandDef {
	return CommandDef{
		Name: "barrier rekey threshold=1",
		Args: []string{
			"barrier", "rekey",
			"--threshold", "1",
			"--total", "3",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"--threshold must be >= 2"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRekeyTotalBelowThresholdCommand asserts the "total >= threshold"
// rule on the rekey verb.
func BarrierRekeyTotalBelowThresholdCommand() CommandDef {
	return CommandDef{
		Name: "barrier rekey total < threshold",
		Args: []string{
			"barrier", "rekey",
			"--threshold", "3",
			"--total", "2",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"--total must be >= --threshold"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Recovery verbs
// =============================================================================

// BarrierRecoveryGenerateCommand builds `barrier recovery generate`.
func BarrierRecoveryGenerateCommand(threshold, total int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("barrier recovery generate %d-of-%d", threshold, total),
		Args: []string{
			"barrier", "recovery", "generate",
			"--threshold", fmt.Sprintf("%d", threshold),
			"--total", fmt.Sprintf("%d", total),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRecoveryGenerateBadThresholdCommand asserts the "threshold >= 2"
// rule.
func BarrierRecoveryGenerateBadThresholdCommand() CommandDef {
	return CommandDef{
		Name: "barrier recovery generate threshold=1",
		Args: []string{
			"barrier", "recovery", "generate",
			"--threshold", "1",
			"--total", "3",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"--threshold must be >= 2"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRecoveryRecoverCommand builds `barrier recovery recover` with a
// repeatable --share flag set.
func BarrierRecoveryRecoverCommand(shares []string) CommandDef {
	args := []string{"barrier", "recovery", "recover"}
	for _, s := range shares {
		args = append(args, "--share", s)
	}
	return CommandDef{
		Name:           fmt.Sprintf("barrier recovery recover (%d shares)", len(shares)),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        45 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// BarrierRecoveryRecoverNoSharesCommand asserts the "at least one share
// required" rule.
func BarrierRecoveryRecoverNoSharesCommand() CommandDef {
	return CommandDef{
		Name:           "barrier recovery recover no shares",
		Args:           []string{"barrier", "recovery", "recover"},
		ExpectSuccess:  false,
		ExpectError:    []string{"--share is required"},
		Timeout:        10 * time.Second,
		SkipTransports: barrierTransportsToSkip,
	}
}

// =============================================================================
// Suites
// =============================================================================

// BarrierInitSuite returns an init -> status -> seal -> unseal lifecycle
// suite for the named strategy. The lifecycle exercises the canonical
// happy path end-to-end so a regression in any verb surfaces in the same
// subtest pass that initialised the daemon.
//
// secret is required for passphrase/shamir strategies and ignored for
// "none". soPin / userPin are forwarded under passphrase/shamir only.
func BarrierInitSuite(strategy, secret, soPin, userPin string) []CommandDef {
	suite := make([]CommandDef, 0, 4)

	if strategy == "none" {
		suite = append(suite, BarrierInitCommand("none", ""))
	} else if soPin != "" && userPin != "" {
		suite = append(suite, BarrierInitWithPINsCommand(strategy, secret, soPin, userPin))
	} else {
		suite = append(suite, BarrierInitCommand(strategy, secret))
	}
	// status MUST report the post-init strategy.
	statusCmd := BarrierStatusCommand()
	statusCmd.ExpectOutput = []string{strategy}
	suite = append(suite, statusCmd)
	return suite
}
