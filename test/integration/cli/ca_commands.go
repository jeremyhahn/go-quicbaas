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

package cli

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// =============================================================================
// CA Commands
// =============================================================================
//
// Builders for the canonical `quicbaas ca <verb>` tree defined in
// cmd/quicbaas/ca.go. Every verb routes through the SDK's
// Security().Certificate() handle, which today is wired in the embedded
// and REST transports only. The gRPC / QUIC / MCP / Unix transports do
// not yet route the quicbaas.Certificate service, so we exclude them at
// the test level via caTransportsToSkip.
//
// Embedded is declared in runner.go but not in GetTransportConfigs(); it
// cannot be exercised through the compiled CLI binary, so for these
// integration tests "all transports" effectively means "all enabled
// network transports that route Certificate". When more transports add
// Certificate routing they can be removed from caTransportsToSkip.

// caTransportsToSkip enumerates transports that do not currently route
// the quicbaas.Certificate service. These transports return an
// Unimplemented / unknown service error for every CA verb, so the
// integration tests skip them rather than asserting against an error
// the SDK is responsible for surfacing.
var caTransportsToSkip = []Transport{
	TransportGRPC,
	TransportQUIC,
	TransportMCP,
	TransportUnix,
}

// caTimeout is the default per-command timeout for CA operations.
const caTimeout = 15 * time.Second

// caSerialTablePattern matches the "Serial Number:" line in the table
// rendering of `ca issue` output. Used by ExtractIssuedSerial.
var caSerialTablePattern = regexp.MustCompile(`Serial Number:[ \t]+(\S+)`)

// caSerialJSONPattern matches the serial_number field in JSON
// rendering of `ca issue` output.
var caSerialJSONPattern = regexp.MustCompile(`"serial_number"\s*:\s*"([^"]+)"`)

// ExtractIssuedSerial parses the serial number returned by `ca issue`
// (either table or JSON output). Returns "" when no serial is present.
func ExtractIssuedSerial(stdout string) string {
	if m := caSerialTablePattern.FindStringSubmatch(stdout); len(m) > 1 {
		// Guard against accidentally matching a label on a subsequent line.
		v := m[1]
		if strings.HasSuffix(v, ":") {
			return ""
		}
		return v
	}
	if m := caSerialJSONPattern.FindStringSubmatch(stdout); len(m) > 1 {
		return m[1]
	}
	return ""
}

// =============================================================================
// info
// =============================================================================

// CAInfoCommand returns a command to display the active issuing CA's
// metadata in the default (table) format.
func CAInfoCommand() CommandDef {
	return CommandDef{
		Name:           "ca info",
		Args:           []string{"ca", "info"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAInfoJSONCommand returns a command to display the CA metadata in
// JSON format.
func CAInfoJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ca info --output json",
		Args:           []string{"ca", "--output", "json", "info"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// =============================================================================
// bundle
// =============================================================================

// CABundleCommand returns a command to print the CA trust-chain PEM
// bundle to stdout in the default (raw PEM) format.
func CABundleCommand() CommandDef {
	return CommandDef{
		Name:           "ca bundle",
		Args:           []string{"ca", "bundle"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CABundleJSONCommand returns a command to fetch the CA bundle in JSON
// format.
func CABundleJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ca bundle --output json",
		Args:           []string{"ca", "--output", "json", "bundle"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// =============================================================================
// list
// =============================================================================

// CAListCommand returns a command to list currently-valid certificates
// in the CA cert store.
func CAListCommand() CommandDef {
	return CommandDef{
		Name:           "ca list",
		Args:           []string{"ca", "list"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListJSONCommand returns a command to list certificates in JSON
// format.
func CAListJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ca list --output json",
		Args:           []string{"ca", "--output", "json", "list"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListWithCommonNameCommand returns a command to list certificates
// filtered by a Common Name substring.
func CAListWithCommonNameCommand(commonName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ca list --common-name=%s", commonName),
		Args:           []string{"ca", "list", "--common-name", commonName},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListIncludeRevokedCommand returns a command to list certificates
// including revoked entries.
func CAListIncludeRevokedCommand() CommandDef {
	return CommandDef{
		Name:           "ca list --include-revoked",
		Args:           []string{"ca", "list", "--include-revoked"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListIncludeExpiredCommand returns a command to list certificates
// including expired entries.
func CAListIncludeExpiredCommand() CommandDef {
	return CommandDef{
		Name:           "ca list --include-expired",
		Args:           []string{"ca", "list", "--include-expired"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListWithLimitCommand returns a command to list certificates capped
// to the supplied limit.
func CAListWithLimitCommand(limit int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ca list --limit=%d", limit),
		Args:           []string{"ca", "list", "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListAllFiltersCommand returns a command exercising every list
// filter simultaneously. The CN filter is a substring; the limit is
// the maximum number of certificates returned.
func CAListAllFiltersCommand(commonName string, limit int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ca list --common-name=%s --include-revoked --include-expired --limit=%d",
			commonName, limit),
		Args: []string{
			"ca", "list",
			"--common-name", commonName,
			"--include-revoked",
			"--include-expired",
			"--limit", fmt.Sprintf("%d", limit),
		},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAListNegativeLimitCommand returns a command that should fail because
// --limit is negative.
func CAListNegativeLimitCommand() CommandDef {
	return CommandDef{
		Name:           "ca list --limit=-1 (error case)",
		Args:           []string{"ca", "list", "--limit", "-1"},
		ExpectSuccess:  false,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// =============================================================================
// issue
// =============================================================================
//
// NOTE: The canonical `ca issue` verb (see cmd/quicbaas/ca.go) generates
// the keypair server-side and accepts subject / SAN / validity flags
// directly. There is no --csr file flag at this layer; the spec's
// "ca issue --csr" wording reflects the in-progress unification spec
// but the implemented surface is --common-name / --dns / --ip /
// --validity-days / --usage / --key-algorithm.

// CAIssueCommand returns a command to issue a fresh certificate with
// just a Common Name. Other parameters fall back to server defaults.
func CAIssueCommand(commonName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ca issue cn=%s", commonName),
		Args:           []string{"ca", "issue", "--common-name", commonName},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueWithValidityCommand returns a command to issue a certificate
// with an explicit validity-days override.
func CAIssueWithValidityCommand(commonName string, validityDays int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ca issue cn=%s validity=%d", commonName, validityDays),
		Args: []string{
			"ca", "issue",
			"--common-name", commonName,
			"--validity-days", fmt.Sprintf("%d", validityDays),
		},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueWithSANCommand returns a command to issue a certificate with
// DNS and IP SAN entries.
func CAIssueWithSANCommand(commonName string, dnsNames, ipAddresses []string) CommandDef {
	args := []string{"ca", "issue", "--common-name", commonName}
	for _, d := range dnsNames {
		args = append(args, "--dns", d)
	}
	for _, ip := range ipAddresses {
		args = append(args, "--ip", ip)
	}
	return CommandDef{
		Name: fmt.Sprintf("ca issue cn=%s dns=%v ip=%v",
			commonName, dnsNames, ipAddresses),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueWithUsageCommand returns a command to issue a certificate
// constrained to a specific usage profile (client | server | both).
func CAIssueWithUsageCommand(commonName, usage string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ca issue cn=%s usage=%s", commonName, usage),
		Args: []string{
			"ca", "issue",
			"--common-name", commonName,
			"--usage", usage,
		},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueWithKeyAlgorithmCommand returns a command to issue a
// certificate signed by a server-generated key of the supplied
// algorithm. The CLI accepts "ecdsa" or "rsa" (case-insensitive).
func CAIssueWithKeyAlgorithmCommand(commonName, algorithm string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ca issue cn=%s key-algorithm=%s", commonName, algorithm),
		Args: []string{
			"ca", "issue",
			"--common-name", commonName,
			"--key-algorithm", algorithm,
		},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueAllFlagsCommand returns a command exercising the full set of
// issue flags in a single invocation.
func CAIssueAllFlagsCommand(commonName, usage, keyAlgorithm string, validityDays int, dnsNames, ipAddresses []string) CommandDef {
	args := []string{
		"ca", "issue",
		"--common-name", commonName,
		"--validity-days", fmt.Sprintf("%d", validityDays),
		"--usage", usage,
		"--key-algorithm", keyAlgorithm,
	}
	for _, d := range dnsNames {
		args = append(args, "--dns", d)
	}
	for _, ip := range ipAddresses {
		args = append(args, "--ip", ip)
	}
	return CommandDef{
		Name:           fmt.Sprintf("ca issue cn=%s (all flags)", commonName),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueJSONCommand returns a command to issue a certificate and
// receive the response in JSON format.
func CAIssueJSONCommand(commonName string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ca issue cn=%s --output json", commonName),
		Args: []string{
			"ca", "--output", "json", "issue",
			"--common-name", commonName,
		},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAIssueMissingCommonNameCommand returns a command that should fail
// because --common-name is missing.
func CAIssueMissingCommonNameCommand() CommandDef {
	return CommandDef{
		Name:           "ca issue (missing --common-name)",
		Args:           []string{"ca", "issue", "--validity-days", "30"},
		ExpectSuccess:  false,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// =============================================================================
// revoke
// =============================================================================

// CARevokeCommand returns a command to revoke a certificate by its
// hex-encoded serial number.
func CARevokeCommand(serial string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ca revoke %s", serial),
		Args:           []string{"ca", "revoke", serial},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CARevokeJSONCommand returns a revoke command rendered as JSON.
func CARevokeJSONCommand(serial string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("ca revoke %s --output json", serial),
		Args: []string{
			"ca", "--output", "json", "revoke", serial,
		},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CARevokeMissingSerialCommand returns a command that should fail
// because the <serial> positional argument is missing. cobra's
// ExactArgs(1) is what surfaces the error.
func CARevokeMissingSerialCommand() CommandDef {
	return CommandDef{
		Name:           "ca revoke (missing serial)",
		Args:           []string{"ca", "revoke"},
		ExpectSuccess:  false,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// =============================================================================
// export-bundle
// =============================================================================

// CAExportBundleCommand returns a command to write the CA bundle PEM
// to the supplied filesystem path.
func CAExportBundleCommand(outputPath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ca export-bundle %s", outputPath),
		Args:           []string{"ca", "export-bundle", outputPath},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAExportBundleStdoutCommand returns a command to write the CA bundle
// PEM to stdout (path == "-").
func CAExportBundleStdoutCommand() CommandDef {
	return CommandDef{
		Name:           "ca export-bundle -",
		Args:           []string{"ca", "export-bundle", "-"},
		ExpectSuccess:  true,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// CAExportBundleMissingPathCommand returns a command that should fail
// because the <path> positional argument is missing.
func CAExportBundleMissingPathCommand() CommandDef {
	return CommandDef{
		Name:           "ca export-bundle (missing path)",
		Args:           []string{"ca", "export-bundle"},
		ExpectSuccess:  false,
		Timeout:        caTimeout,
		RequiresUnseal: true,
		SkipTransports: caTransportsToSkip,
	}
}

// =============================================================================
// CA Suites
// =============================================================================

// CAReadOnlySuite returns the read-only CA verbs that should succeed
// against any provisioned daemon without modifying state.
func CAReadOnlySuite() []CommandDef {
	return []CommandDef{
		CAInfoCommand(),
		CABundleCommand(),
		CAListCommand(),
		CAListIncludeRevokedCommand(),
		CAListIncludeExpiredCommand(),
	}
}

// CAErrorSuite returns the set of CA verbs that exercise required-flag
// and required-arg validation paths. Each command is expected to fail
// fast in the CLI (before any RPC is dispatched) with a clear error.
func CAErrorSuite() []CommandDef {
	return []CommandDef{
		CAIssueMissingCommonNameCommand(),
		CARevokeMissingSerialCommand(),
		CAExportBundleMissingPathCommand(),
		CAListNegativeLimitCommand(),
	}
}
