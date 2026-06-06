// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package cli

import (
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Ceremony Transport Configuration
// =============================================================================

// ceremonyTransportsToSkip lists transports where the ceremony verbs are not
// reachable via the active CLI wiring in the integration cluster.
//
// The CLI routes Security().Ceremony() through the QRDB SDK, which exposes
// a transport per protocol (rest / grpc / quic / unix / mcp / embedded). Each
// transport is wired through to the QuicBaaS daemon's /api/v1/init surface
// (REST), CeremonyServer (gRPC/Unix), OpCeremony sub-op (QUIC), and
// quicbaas_ceremony_* MCP tools. The daemon serves all of them, so no
// transport is unconditionally skipped here — runtime probes handle the case
// where the ceremony service is unwired in a given deployment.
var ceremonyTransportsToSkip []Transport

// =============================================================================
// Ceremony Service Availability Probe
// =============================================================================

// RequireCeremonyAvailable probes the server to verify that the ceremony
// service is wired. The daemon returns "service unavailable" (or a transport-
// equivalent error) for every ceremony verb when the service has not been
// attached at lifecycle time. This probe lets read-only tests skip cleanly
// when running against a cluster that has not been configured for ceremony.
//
// Returns true when the ceremony surface is reachable, false when the server
// reports unavailability (the test should then return early without failing).
func (r *TestRunner) RequireCeremonyAvailable() bool {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:           "ceremony service availability probe",
		Args:           []string{"ceremony", "status"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}

	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		combined := strings.TrimSpace(stdout + stderr)
		r.t.Logf("[%s] Ceremony service not enabled on server (validated correct error response: %s)",
			r.transport, combined)
		return false
	}
	return true
}

// =============================================================================
// Ceremony Status Commands
// =============================================================================

// CeremonyStatusCommand returns a command to read the current ceremony state
// (default table output).
func CeremonyStatusCommand() CommandDef {
	return CommandDef{
		Name:           "ceremony status",
		Args:           []string{"ceremony", "status"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyStatusJSONCommand returns a command to read ceremony state with
// JSON output. The persistent --output flag on the ceremony group accepts
// "json", "yaml", and "table".
func CeremonyStatusJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ceremony status json",
		Args:           []string{"ceremony", "status", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyStatusFormatCommand returns a status command with an explicit
// --output format value (json | table | yaml).
func CeremonyStatusFormatCommand(format string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ceremony status --output %s", format),
		Args:           []string{"ceremony", "status", "--output", format},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// =============================================================================
// Ceremony Claim-Cert Commands
// =============================================================================

// CeremonyClaimCertCommand returns a claim-cert command with the standard
// flag set. The --output flag is a FILE PATH on this verb (overriding the
// parent's persistent format flag), so callers pass the desired cert output
// path here.
func CeremonyClaimCertCommand(username, csrKeyPath, outputPath string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ceremony claim-cert %s", username),
		Args: []string{
			"ceremony", "claim-cert",
			"--username", username,
			"--csr-key", csrKeyPath,
			"--output", outputPath,
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyClaimCertMissingFlagCommand returns a claim-cert command that is
// missing one of the required flags. Used to drive required-flag validation
// tests for the verb. The omittedFlag value identifies which flag to leave
// out: "username", "csr-key", or "output".
func CeremonyClaimCertMissingFlagCommand(omittedFlag string) CommandDef {
	args := []string{"ceremony", "claim-cert"}
	if omittedFlag != "username" {
		args = append(args, "--username", "alice")
	}
	if omittedFlag != "csr-key" {
		args = append(args, "--csr-key", "/tmp/nonexistent.key")
	}
	if omittedFlag != "output" {
		args = append(args, "--output", "/tmp/nonexistent.crt")
	}
	return CommandDef{
		Name:           fmt.Sprintf("ceremony claim-cert missing --%s", omittedFlag),
		Args:           args,
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// =============================================================================
// Ceremony Claim-Share Commands
// =============================================================================

// CeremonyClaimShareCommand returns a claim-share command with the standard
// flag set. Like claim-cert, --output here is a FILE PATH (the share JSON
// destination), not the persistent format flag.
func CeremonyClaimShareCommand(username, tlsCertPath, tlsKeyPath, outputPath string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ceremony claim-share %s", username),
		Args: []string{
			"ceremony", "claim-share",
			"--username", username,
			"--tls-cert", tlsCertPath,
			"--tls-key", tlsKeyPath,
			"--output", outputPath,
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyClaimShareMissingFlagCommand returns a claim-share command that
// is missing one of the required flags. omittedFlag is one of "username",
// "tls-cert", "tls-key", or "output".
func CeremonyClaimShareMissingFlagCommand(omittedFlag string) CommandDef {
	args := []string{"ceremony", "claim-share"}
	if omittedFlag != "username" {
		args = append(args, "--username", "alice")
	}
	if omittedFlag != "tls-cert" {
		args = append(args, "--tls-cert", "/tmp/nonexistent.crt")
	}
	if omittedFlag != "tls-key" {
		args = append(args, "--tls-key", "/tmp/nonexistent.key")
	}
	if omittedFlag != "output" {
		args = append(args, "--output", "/tmp/nonexistent.share.json")
	}
	return CommandDef{
		Name:           fmt.Sprintf("ceremony claim-share missing --%s", omittedFlag),
		Args:           args,
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// =============================================================================
// Ceremony Sign-CSR Commands
// =============================================================================

// CeremonySignCSRCommand returns a sign-csr command with the standard flag
// set. The signed certificate is printed to stdout (PEM).
func CeremonySignCSRCommand(username, soPin, csrPath, role string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ceremony sign-csr %s", username),
		Args: []string{
			"ceremony", "sign-csr",
			"--username", username,
			"--so-pin", soPin,
			"--csr", csrPath,
			"--role", role,
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonySignCSRMissingFlagCommand returns a sign-csr command that is
// missing one of the required flags. omittedFlag is one of "username",
// "so-pin", "csr", or "role".
func CeremonySignCSRMissingFlagCommand(omittedFlag string) CommandDef {
	args := []string{"ceremony", "sign-csr"}
	if omittedFlag != "username" {
		args = append(args, "--username", "alice")
	}
	if omittedFlag != "so-pin" {
		args = append(args, "--so-pin", "dummy-pin")
	}
	if omittedFlag != "csr" {
		args = append(args, "--csr", "/tmp/nonexistent.csr")
	}
	if omittedFlag != "role" {
		args = append(args, "--role", "officer")
	}
	return CommandDef{
		Name:           fmt.Sprintf("ceremony sign-csr missing --%s", omittedFlag),
		Args:           args,
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// =============================================================================
// Ceremony Enroll-External-Officer Commands
// =============================================================================

// CeremonyEnrollExternalOfficerCertCommand returns an enroll-external-officer
// command using a certificate (--cert) for the officer's external identity.
// --cert and --pubkey are mutually exclusive; use this builder for the cert
// variant.
func CeremonyEnrollExternalOfficerCertCommand(username, certPath string, shareIndex int, authFactor string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ceremony enroll-external-officer %s (cert)", username),
		Args: []string{
			"ceremony", "enroll-external-officer",
			"--username", username,
			"--cert", certPath,
			"--share-index", fmt.Sprintf("%d", shareIndex),
			"--auth-factor", authFactor,
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyEnrollExternalOfficerPubKeyCommand returns an enroll-external-
// officer command using a public key (--pubkey) for the officer's external
// identity. --cert and --pubkey are mutually exclusive; use this builder for
// the pubkey variant.
func CeremonyEnrollExternalOfficerPubKeyCommand(username, pubKeyPath string, shareIndex int, authFactor string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ceremony enroll-external-officer %s (pubkey)", username),
		Args: []string{
			"ceremony", "enroll-external-officer",
			"--username", username,
			"--pubkey", pubKeyPath,
			"--share-index", fmt.Sprintf("%d", shareIndex),
			"--auth-factor", authFactor,
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyEnrollExternalOfficerMissingUsernameCommand returns an enroll
// command that omits --username (the only flag whose absence is a pure
// client-side validation failure independent of file I/O).
func CeremonyEnrollExternalOfficerMissingUsernameCommand() CommandDef {
	return CommandDef{
		Name: "ceremony enroll-external-officer missing --username",
		Args: []string{
			"ceremony", "enroll-external-officer",
			"--cert", "/tmp/nonexistent.crt",
			"--share-index", "1",
			"--auth-factor", "piv",
		},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyEnrollExternalOfficerBothCertAndPubKeyCommand returns an enroll
// command that supplies both --cert and --pubkey, which is mutually exclusive
// and must fail validation.
func CeremonyEnrollExternalOfficerBothCertAndPubKeyCommand() CommandDef {
	return CommandDef{
		Name: "ceremony enroll-external-officer both --cert and --pubkey",
		Args: []string{
			"ceremony", "enroll-external-officer",
			"--username", "alice",
			"--cert", "/tmp/nonexistent.crt",
			"--pubkey", "/tmp/nonexistent.pub",
			"--share-index", "1",
			"--auth-factor", "piv",
		},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyEnrollExternalOfficerNeitherCertNorPubKeyCommand returns an enroll
// command that supplies neither --cert nor --pubkey, which is required and
// must fail validation.
func CeremonyEnrollExternalOfficerNeitherCertNorPubKeyCommand() CommandDef {
	return CommandDef{
		Name: "ceremony enroll-external-officer no --cert/--pubkey",
		Args: []string{
			"ceremony", "enroll-external-officer",
			"--username", "alice",
			"--share-index", "1",
			"--auth-factor", "piv",
		},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyEnrollExternalOfficerInvalidAuthFactorCommand returns an enroll
// command with an --auth-factor value outside the allowed set (mtls / webauthn
// / piv). The CLI must reject this without contacting the server.
func CeremonyEnrollExternalOfficerInvalidAuthFactorCommand() CommandDef {
	return CommandDef{
		Name: "ceremony enroll-external-officer invalid --auth-factor",
		Args: []string{
			"ceremony", "enroll-external-officer",
			"--username", "alice",
			"--cert", "/tmp/nonexistent.crt",
			"--share-index", "1",
			"--auth-factor", "not-a-real-factor",
		},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// =============================================================================
// Help / Group Discovery
// =============================================================================

// CeremonyHelpCommand returns the top-level ceremony group --help command.
func CeremonyHelpCommand() CommandDef {
	return CommandDef{
		Name:           "ceremony --help",
		Args:           []string{"ceremony", "--help"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"status", "claim-cert", "claim-share", "sign-csr", "enroll-external-officer"},
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}

// CeremonyVerbHelpCommand returns the --help command for an individual
// ceremony verb. The expected output substrings are encoded per verb so the
// test catches accidental flag deletions.
func CeremonyVerbHelpCommand(verb string) CommandDef {
	expect := map[string][]string{
		"status":                  {"State", "ceremony"},
		"claim-cert":              {"--username", "--csr-key", "--output"},
		"claim-share":             {"--username", "--tls-cert", "--tls-key", "--output"},
		"sign-csr":                {"--username", "--so-pin", "--csr", "--role"},
		"enroll-external-officer": {"--username", "--cert", "--pubkey", "--share-index", "--auth-factor"},
	}
	return CommandDef{
		Name:           fmt.Sprintf("ceremony %s --help", verb),
		Args:           []string{"ceremony", verb, "--help"},
		ExpectSuccess:  true,
		ExpectOutput:   expect[verb],
		Timeout:        10 * time.Second,
		SkipTransports: ceremonyTransportsToSkip,
	}
}
