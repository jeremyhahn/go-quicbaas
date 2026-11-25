// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package cli — credential CLI verb command factories.
//
// The canonical "credential" verb tree (per
// docs/08-bootstrap-auth-architecture.md §6 and cmd/quicbaas/credential.go)
// delegates to Security().Credentials() over the active SDK transport.
// Every transport ships a real-wire implementation, so the credential
// verbs are exercised across every transport returned by
// GetTransportConfigs() — no SkipTransports list is required when the
// node serves credentials. Tests probe with RequireCredentialAvailable
// first and skip on a seal-off node, which wires no credential service.
//
// Verb surface:
//
//	credential submit   <name> --value <v>            non-versioned set/replace
//	credential list                                   names only
//	credential rotate   <name> --value <v> [--grace]  versioned rotation
//	credential retrieve <name> [--version N]          read current or specific version
//	credential versions <name>                        metadata (current + grace history)
//	credential delete   <name>                        wipe credential + history
//	credential gc       <name>                        reap expired grace versions
package cli

import (
	"fmt"
	"strings"
	"time"
)

// credentialAuthRefusals lists the 401/403 texts the credential legs print
// that authRefusalIndicators does not list (the SDK REST/QUIC client prints
// "(status 401)", the MCP client "unauthorized (401)", and typed errors carry
// the UNAUTHORIZED/FORBIDDEN code names). A refusal is a real auth failure and
// must never be read as the not-configured answer.
var credentialAuthRefusals = []string{
	"status 401",
	"status 403",
	"(401)",
	"(403)",
	"UNAUTHORIZED",
	"FORBIDDEN",
	"unauthorized",
	"forbidden",
}

// credentialNotConfiguredAnswers lists, per transport, the not-configured
// texts that transport is allowed to print when the node wires no credential
// service. A text is accepted only on the transport that prints it, so a REST
// 404 shape on the gRPC leg is not accepted.
var credentialNotConfiguredAnswers = map[Transport][]string{
	TransportGRPC: {
		"credential service is not configured",
	},
	TransportUnix: {
		"credential service is not configured",
	},
	TransportREST: {
		"credential service is not configured",
		"credential service not configured",
		"NOT_FOUND: rest.",
		"HTTP 404",
		"(status 404)",
		"404 page not found",
	},
	TransportQUIC: {
		"credential service is not configured",
		"credential service not configured",
		"NOT_FOUND: rest.",
		"HTTP 404",
		"(status 404)",
		"404 page not found",
	},
	TransportMCP: {
		"credential service not configured",
		"credential service is not configured",
		"tool 'qrdb_credential_list' not found",
		"tool not found",
	},
}

// credentialNotConfiguredAnswer reports whether the probe output is the
// typed not-configured answer for transport. Any 401/403 refusal text
// (authRefusalIndicators or credentialAuthRefusals) wins and yields false.
// A transport with no entry (for example embedded) yields false.
func credentialNotConfiguredAnswer(transport Transport, stdout, stderr string) bool {
	combined := stdout + stderr
	for _, refusal := range authRefusalIndicators {
		if strings.Contains(combined, refusal) {
			return false
		}
	}
	for _, refusal := range credentialAuthRefusals {
		if strings.Contains(combined, refusal) {
			return false
		}
	}
	for _, answer := range credentialNotConfiguredAnswers[transport] {
		if strings.Contains(combined, answer) {
			return true
		}
	}
	return false
}

// RequireCredentialAvailable probes the credential service with the
// read-only `credential list` and skips the current transport leg when the
// node answers not-configured. On a seal-off node no credential service is
// wired, by owner ruling, so every credential verb answers not-configured
// (gRPC/unix), 404 (REST/QUIC) or not-configured/tool-not-found (MCP). The
// probe validates that typed answer per transport and skips. It never
// accepts a 401 or 403: any other failure returns, so the test goes on and
// fails on its own commands.
func (r *TestRunner) RequireCredentialAvailable() {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:          "credential service availability probe",
		Args:          []string{"credential", "list"},
		ExpectSuccess: true,
		Timeout:       15 * time.Second,
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err == nil {
		return
	}
	if credentialNotConfiguredAnswer(r.transport, stdout, stderr) {
		r.t.Skipf("[%s] credential service not configured on this node (seal-off; wiring a credential service is deferred to the owner); validated the not-configured answer: %s",
			r.transport, strings.TrimSpace(stdout+stderr))
	}
}

// =============================================================================
// credential submit
// =============================================================================

// CredentialSubmitCommand returns a command that submits a credential
// value under the given name. Submit is the non-versioned set/replace
// path — use CredentialRotateCommand for versioned rotation with a
// grace window.
func CredentialSubmitCommand(name, value string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential submit %s", name),
		Args:          []string{"credential", "submit", name, "--value", value},
		ExpectSuccess: true,
		ExpectOutput:  []string{name, "submitted"},
		Timeout:       10 * time.Second,
	}
}

// CredentialSubmitMissingNameCommand returns a submit command with no
// positional <name> argument. Cobra's ExactArgs(1) enforcement makes
// this fail before any transport round-trip.
func CredentialSubmitMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "credential submit (missing name)",
		Args:          []string{"credential", "submit", "--value", "some-value"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// CredentialSubmitMissingValueCommand returns a submit command that
// supplies <name> but omits --value. The CLI returns
// CredentialValueRequiredError before invoking the transport.
func CredentialSubmitMissingValueCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential submit %s (missing --value)", name),
		Args:          []string{"credential", "submit", name},
		ExpectSuccess: false,
		ExpectError:   []string{"--value is required"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// credential list
// =============================================================================

// CredentialListCommand returns a command that enumerates every stored
// credential by name. Values are never returned by enumeration.
func CredentialListCommand() CommandDef {
	return CommandDef{
		Name:          "credential list",
		Args:          []string{"credential", "list"},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// CredentialListJSONCommand returns the list command with --output json.
// The JSON envelope is {"names":[...]} per writeCredentialNames.
func CredentialListJSONCommand() CommandDef {
	return CommandDef{
		Name:          "credential list (json)",
		Args:          []string{"credential", "list", "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{`"names"`},
		Timeout:       10 * time.Second,
	}
}

// CredentialListTableCommand returns the list command with --output table.
func CredentialListTableCommand() CommandDef {
	return CommandDef{
		Name:          "credential list (table)",
		Args:          []string{"credential", "list", "--output", "table"},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// CredentialListExpectingCommand returns a list command that asserts the
// given names are present in stdout. Used by round-trip tests to verify
// presence after submit and absence after delete.
func CredentialListExpectingCommand(expectedNames ...string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential list expecting %v", expectedNames),
		Args:          []string{"credential", "list"},
		ExpectSuccess: true,
		ExpectOutput:  expectedNames,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// credential rotate
// =============================================================================

// CredentialRotateCommand returns a versioned rotation with optional
// grace. A zero grace means "wipe previous version immediately".
func CredentialRotateCommand(name, newValue string, grace time.Duration) CommandDef {
	args := []string{"credential", "rotate", name, "--value", newValue}
	if grace > 0 {
		args = append(args, "--grace", grace.String())
	}
	return CommandDef{
		Name:          fmt.Sprintf("credential rotate %s grace=%s", name, grace),
		Args:          args,
		ExpectSuccess: true,
		ExpectOutput:  []string{name},
		Timeout:       10 * time.Second,
	}
}

// CredentialRotateJSONCommand returns the rotate command with
// --output json. The JSON envelope is the wire CredentialRotateResponse
// shape (name + previous_version + new_version + grace_until_unix).
func CredentialRotateJSONCommand(name, newValue string, grace time.Duration) CommandDef {
	args := []string{"credential", "rotate", name, "--value", newValue, "--output", "json"}
	if grace > 0 {
		args = append(args, "--grace", grace.String())
	}
	return CommandDef{
		Name:          fmt.Sprintf("credential rotate %s (json) grace=%s", name, grace),
		Args:          args,
		ExpectSuccess: true,
		ExpectOutput:  []string{`"name"`, `"previous_version"`, `"new_version"`},
		Timeout:       10 * time.Second,
	}
}

// CredentialRotateMissingNameCommand returns a rotate command missing
// the positional <name> argument. cobra.ExactArgs(1) makes this fail
// before any transport round-trip.
func CredentialRotateMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "credential rotate (missing name)",
		Args:          []string{"credential", "rotate", "--value", "new-value"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// CredentialRotateMissingValueCommand returns a rotate command that
// supplies <name> but omits --value.
func CredentialRotateMissingValueCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential rotate %s (missing --value)", name),
		Args:          []string{"credential", "rotate", name},
		ExpectSuccess: false,
		ExpectError:   []string{"--value is required"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// credential retrieve
// =============================================================================

// CredentialRetrieveCommand returns a retrieve command for the current
// version. The retrieve verb writes only the raw value to stdout, so
// callers can match the value with ExpectOutput.
func CredentialRetrieveCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential retrieve %s", name),
		Args:          []string{"credential", "retrieve", name},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// CredentialRetrieveExpectingValueCommand returns a retrieve command
// that asserts the printed value matches the expected string.
func CredentialRetrieveExpectingValueCommand(name, expectedValue string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential retrieve %s == %q", name, expectedValue),
		Args:          []string{"credential", "retrieve", name},
		ExpectSuccess: true,
		ExpectOutput:  []string{expectedValue},
		Timeout:       10 * time.Second,
	}
}

// CredentialRetrieveVersionCommand returns a retrieve command for a
// specific historical version. Version 0 means "current".
func CredentialRetrieveVersionCommand(name string, version uint64) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential retrieve %s --version %d", name, version),
		Args:          []string{"credential", "retrieve", name, "--version", fmt.Sprintf("%d", version)},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// CredentialRetrieveVersionExpectingValueCommand returns a retrieve
// command that pins both the version and the expected value.
func CredentialRetrieveVersionExpectingValueCommand(name string, version uint64, expectedValue string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential retrieve %s v%d == %q", name, version, expectedValue),
		Args:          []string{"credential", "retrieve", name, "--version", fmt.Sprintf("%d", version)},
		ExpectSuccess: true,
		ExpectOutput:  []string{expectedValue},
		Timeout:       10 * time.Second,
	}
}

// CredentialRetrieveExpectFailCommand returns a retrieve command that
// is expected to fail (e.g., after delete or after grace expiry).
func CredentialRetrieveExpectFailCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential retrieve %s (expect fail)", name),
		Args:          []string{"credential", "retrieve", name},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// CredentialRetrieveVersionExpectFailCommand returns a versioned
// retrieve that is expected to fail (e.g., the requested version has
// expired or was wiped).
func CredentialRetrieveVersionExpectFailCommand(name string, version uint64) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential retrieve %s --version %d (expect fail)", name, version),
		Args:          []string{"credential", "retrieve", name, "--version", fmt.Sprintf("%d", version)},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// CredentialRetrieveMissingNameCommand returns a retrieve command
// missing the positional <name> argument.
func CredentialRetrieveMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "credential retrieve (missing name)",
		Args:          []string{"credential", "retrieve"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// credential versions
// =============================================================================

// CredentialVersionsCommand returns a command that prints the version
// metadata table for the given credential.
func CredentialVersionsCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential versions %s", name),
		Args:          []string{"credential", "versions", name},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// CredentialVersionsJSONCommand returns the versions command with
// --output json. The JSON envelope is CredentialListVersionsResponse
// (name + array of CredentialVersionInfo).
func CredentialVersionsJSONCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential versions %s (json)", name),
		Args:          []string{"credential", "versions", name, "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{`"versions"`},
		Timeout:       10 * time.Second,
	}
}

// CredentialVersionsTableCommand returns the versions command with the
// default table output explicitly selected.
func CredentialVersionsTableCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential versions %s (table)", name),
		Args:          []string{"credential", "versions", name, "--output", "table"},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// CredentialVersionsMissingNameCommand returns a versions command
// missing the positional <name> argument.
func CredentialVersionsMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "credential versions (missing name)",
		Args:          []string{"credential", "versions"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// credential delete
// =============================================================================

// CredentialDeleteCommand returns a command that deletes a credential
// and all of its historical versions.
func CredentialDeleteCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential delete %s", name),
		Args:          []string{"credential", "delete", name},
		ExpectSuccess: true,
		ExpectOutput:  []string{name, "deleted"},
		Timeout:       10 * time.Second,
	}
}

// CredentialDeleteMissingNameCommand returns a delete command missing
// the positional <name> argument.
func CredentialDeleteMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "credential delete (missing name)",
		Args:          []string{"credential", "delete"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// credential gc
// =============================================================================

// CredentialGCCommand returns a command that reaps expired grace
// versions of the given credential.
func CredentialGCCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("credential gc %s", name),
		Args:          []string{"credential", "gc", name},
		ExpectSuccess: true,
		ExpectOutput:  []string{"reaped"},
		Timeout:       10 * time.Second,
	}
}

// CredentialGCMissingNameCommand returns a gc command missing the
// positional <name> argument.
func CredentialGCMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "credential gc (missing name)",
		Args:          []string{"credential", "gc"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Suites
// =============================================================================

// CredentialSuite returns the full canonical-verb suite (submit, list,
// versions, retrieve, rotate, delete, gc) for the given name + value.
// Use with TestRunner.RunSuite for a one-shot end-to-end smoke test.
func CredentialSuite(name, value string) []CommandDef {
	rotated := value + "-rotated"
	return []CommandDef{
		CredentialSubmitCommand(name, value),
		CredentialListExpectingCommand(name),
		CredentialRetrieveExpectingValueCommand(name, value),
		CredentialVersionsCommand(name),
		CredentialRotateCommand(name, rotated, 0),
		CredentialRetrieveExpectingValueCommand(name, rotated),
		CredentialGCCommand(name),
		CredentialDeleteCommand(name),
	}
}

// CredentialErrorSuite returns the missing-arg error cases for every
// verb that requires positional <name> and/or --value.
func CredentialErrorSuite() []CommandDef {
	return []CommandDef{
		CredentialSubmitMissingNameCommand(),
		CredentialSubmitMissingValueCommand("err-no-value"),
		CredentialRotateMissingNameCommand(),
		CredentialRotateMissingValueCommand("err-no-value"),
		CredentialRetrieveMissingNameCommand(),
		CredentialVersionsMissingNameCommand(),
		CredentialDeleteMissingNameCommand(),
		CredentialGCMissingNameCommand(),
	}
}
