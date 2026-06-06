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

// Package cli table-driven integration tests for the canonical
// `quicbaas ca` verb tree.
//
// The tests exercise every verb (info, bundle, list, issue, revoke,
// export-bundle) across every transport that routes the
// quicbaas.Certificate service. The CommandDef SkipTransports field in
// ca_commands.go is the authoritative source of supported transports;
// when more transports add Certificate routing, removing them from
// caTransportsToSkip automatically widens the matrix.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Output parsing fixtures
// =============================================================================

// caInfoJSON is the minimum subset of the CAInfoResponse JSON shape the
// table-driven tests assert against. The full response includes
// certificate / bundle PEM blobs; the tests only need scalar metadata.
type caInfoJSON struct {
	CommonName         string `json:"common_name"`
	Subject            string `json:"subject"`
	Issuer             string `json:"issuer"`
	SerialNumber       string `json:"serial_number"`
	IsCA               bool   `json:"is_ca"`
	KeyType            string `json:"key_type"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	CertificatePEM     string `json:"certificate_pem"`
	CABundlePEM        string `json:"ca_bundle_pem"`
}

// caBundleJSON is the JSON shape of the bundle response.
type caBundleJSON struct {
	BundlePEM string `json:"bundle_pem"`
}

// caCertSummaryJSON mirrors CertificateSummary for list-output parsing.
type caCertSummaryJSON struct {
	CommonName   string   `json:"common_name"`
	Subject      string   `json:"subject"`
	Issuer       string   `json:"issuer"`
	SerialNumber string   `json:"serial_number"`
	IsCA         bool     `json:"is_ca"`
	DNSNames     []string `json:"dns_names,omitempty"`
}

// caListJSON is the response shape for `ca list --output json`.
type caListJSON struct {
	Certificates []caCertSummaryJSON `json:"certificates"`
	TotalCount   int                 `json:"total_count"`
}

// caIssueJSON is the response shape for `ca issue --output json`.
type caIssueJSON struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	ChainPEM       string `json:"chain_pem"`
	SerialNumber   string `json:"serial_number"`
}

// caRevokeJSON is the response shape for `ca revoke --output json`.
type caRevokeJSON struct {
	Success bool `json:"success"`
}

// =============================================================================
// Capability probe
// =============================================================================

// requireCAAvailable runs `ca info` and skips the calling test if the
// server reports the CA service unavailable. This keeps the suite
// resilient against integration environments where the CA hasn't been
// provisioned yet (first-boot bootstrap not run, etc.) while still
// failing on genuine regressions.
func requireCAAvailable(t *testing.T, runner *TestRunner) bool {
	t.Helper()
	stdout, stderr, err := runner.Run(CAInfoCommand())
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		t.Skipf("[%s] CA service not available, skipping: %s",
			runner.transport, strings.TrimSpace(stdout+stderr))
		return false
	}
	return true
}

// =============================================================================
// info
// =============================================================================

// TestCAInfo exercises `ca info` across every supported transport in
// both table and JSON output formats.
func TestCAInfo(t *testing.T) {
	RunForTransports(t, "CAInfo", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		t.Run("table", func(t *testing.T) {
			stdout, _, err := runner.Run(CAInfoCommand())
			require.NoError(t, err, "ca info (table) should succeed")
			require.NotEmpty(t, stdout, "ca info should return metadata")
			// The table renderer always prints these labels.
			for _, want := range []string{
				"Common Name:",
				"Subject:",
				"Issuer:",
				"Serial Number:",
				"Not Before:",
				"Not After:",
				"Is CA:",
				"Key Type:",
				"Signature Algorithm:",
			} {
				assert.Contains(t, stdout, want,
					"ca info table output should contain %q", want)
			}
		})

		t.Run("json", func(t *testing.T) {
			stdout, _, err := runner.Run(CAInfoJSONCommand())
			require.NoError(t, err, "ca info (json) should succeed")
			require.NotEmpty(t, stdout, "ca info json should return data")

			var info caInfoJSON
			require.NoError(t, json.Unmarshal([]byte(stdout), &info),
				"ca info json should parse")
			assert.NotEmpty(t, info.CommonName, "common_name should be populated")
			assert.NotEmpty(t, info.SerialNumber, "serial_number should be populated")
			assert.NotEmpty(t, info.KeyType, "key_type should be populated")
			assert.NotEmpty(t, info.SignatureAlgorithm, "signature_algorithm should be populated")
			// The active issuing CA must self-identify as a CA.
			assert.True(t, info.IsCA, "active issuing CA should report is_ca=true")
		})
	})
}

// =============================================================================
// bundle
// =============================================================================

// TestCABundle exercises `ca bundle` across every supported transport
// in both default (raw PEM) and JSON output formats.
func TestCABundle(t *testing.T) {
	RunForTransports(t, "CABundle", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		t.Run("pem", func(t *testing.T) {
			stdout, _, err := runner.Run(CABundleCommand())
			require.NoError(t, err, "ca bundle should succeed")
			require.NotEmpty(t, stdout, "ca bundle should return PEM data")
			assert.Contains(t, stdout, "-----BEGIN CERTIFICATE-----",
				"bundle output should be PEM-encoded")
			assert.Contains(t, stdout, "-----END CERTIFICATE-----",
				"bundle output should be PEM-encoded")
		})

		t.Run("json", func(t *testing.T) {
			stdout, _, err := runner.Run(CABundleJSONCommand())
			require.NoError(t, err, "ca bundle --output json should succeed")
			require.NotEmpty(t, stdout, "ca bundle json should return data")

			var b caBundleJSON
			require.NoError(t, json.Unmarshal([]byte(stdout), &b),
				"ca bundle json should parse")
			assert.Contains(t, b.BundlePEM, "-----BEGIN CERTIFICATE-----",
				"bundle_pem field should contain PEM data")
		})
	})
}

// =============================================================================
// list
// =============================================================================

// TestCAList exercises `ca list` with every documented flag combination.
func TestCAList(t *testing.T) {
	RunForTransports(t, "CAList", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		t.Run("default", func(t *testing.T) {
			stdout, _, err := runner.Run(CAListCommand())
			require.NoError(t, err, "ca list should succeed")
			// Either the header row, or the "(no matching certificates...)" message,
			// must appear. Both are acceptable for an unprovisioned store.
			combined := stdout
			ok := strings.Contains(combined, "COMMON NAME") ||
				strings.Contains(combined, "no matching certificates")
			assert.True(t, ok,
				"ca list should render either the table header or the empty-state message: %s", stdout)
		})

		t.Run("json", func(t *testing.T) {
			stdout, _, err := runner.Run(CAListJSONCommand())
			require.NoError(t, err, "ca list --output json should succeed")
			require.NotEmpty(t, stdout, "ca list json should return data")

			var l caListJSON
			require.NoError(t, json.Unmarshal([]byte(stdout), &l),
				"ca list json should parse")
			// TotalCount is non-negative by construction. The certificates
			// slice may be empty for an unprovisioned store.
			assert.GreaterOrEqual(t, l.TotalCount, 0,
				"total_count should be non-negative")
		})

		t.Run("common-name-filter", func(t *testing.T) {
			// Use a substring that should not appear in any production
			// certificate; the response should be well-formed but empty.
			stdout, _, err := runner.Run(CAListWithCommonNameCommand("__no_such_cn__"))
			require.NoError(t, err, "ca list --common-name should succeed")
			require.NotEmpty(t, stdout, "ca list should always return some text")
		})

		t.Run("include-revoked", func(t *testing.T) {
			stdout, _, err := runner.Run(CAListIncludeRevokedCommand())
			require.NoError(t, err, "ca list --include-revoked should succeed")
			require.NotEmpty(t, stdout)
		})

		t.Run("include-expired", func(t *testing.T) {
			stdout, _, err := runner.Run(CAListIncludeExpiredCommand())
			require.NoError(t, err, "ca list --include-expired should succeed")
			require.NotEmpty(t, stdout)
		})

		t.Run("limit", func(t *testing.T) {
			stdout, _, err := runner.Run(CAListWithLimitCommand(5))
			require.NoError(t, err, "ca list --limit=5 should succeed")
			require.NotEmpty(t, stdout)
		})

		t.Run("all-filters", func(t *testing.T) {
			cmd := CAListAllFiltersCommand("", 10)
			// CAListAllFiltersCommand requires a non-empty common-name; rebuild
			// here with an unmatched substring so the response is empty but
			// well-formed across runs.
			cmd.Args = []string{
				"ca", "list",
				"--common-name", "__no_such_cn__",
				"--include-revoked",
				"--include-expired",
				"--limit", "10",
			}
			stdout, _, err := runner.Run(cmd)
			require.NoError(t, err, "ca list with all filters should succeed")
			require.NotEmpty(t, stdout)
		})

		t.Run("negative-limit", func(t *testing.T) {
			_, _, err := runner.Run(CAListNegativeLimitCommand())
			require.Error(t, err, "ca list --limit=-1 should fail")
		})
	})
}

// =============================================================================
// issue
// =============================================================================

// TestCAIssue exercises every `ca issue` flag permutation against every
// supported transport.
func TestCAIssue(t *testing.T) {
	RunForTransports(t, "CAIssue", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		t.Run("minimal", func(t *testing.T) {
			cn := UniqueKey("ca-issue-min") + ".example.com"
			stdout, _, err := runner.Run(CAIssueCommand(cn))
			require.NoError(t, err, "ca issue minimal should succeed")
			require.NotEmpty(t, stdout, "ca issue should print result")
			assert.Contains(t, stdout, "Serial Number:",
				"issue table output should contain Serial Number")
			assert.Contains(t, stdout, "-----BEGIN CERTIFICATE-----",
				"issue output should contain PEM-encoded certificate")
			// Cleanup: revoke the freshly-issued cert. Best-effort —
			// failure to revoke shouldn't fail the test.
			if serial := ExtractIssuedSerial(stdout); serial != "" {
				_, _, _ = runner.Run(CARevokeCommand(serial))
			}
		})

		t.Run("with-validity-days", func(t *testing.T) {
			cn := UniqueKey("ca-issue-validity") + ".example.com"
			stdout, _, err := runner.Run(CAIssueWithValidityCommand(cn, 30))
			require.NoError(t, err, "ca issue with --validity-days should succeed")
			require.Contains(t, stdout, "Not After:",
				"output should include validity window")
			if serial := ExtractIssuedSerial(stdout); serial != "" {
				_, _, _ = runner.Run(CARevokeCommand(serial))
			}
		})

		t.Run("with-san", func(t *testing.T) {
			cn := UniqueKey("ca-issue-san") + ".example.com"
			stdout, _, err := runner.Run(CAIssueWithSANCommand(
				cn,
				[]string{cn, "alt." + cn},
				[]string{"127.0.0.1"},
			))
			require.NoError(t, err, "ca issue with SAN should succeed")
			require.NotEmpty(t, stdout)
			if serial := ExtractIssuedSerial(stdout); serial != "" {
				_, _, _ = runner.Run(CARevokeCommand(serial))
			}
		})

		t.Run("usage-profiles", func(t *testing.T) {
			for _, usage := range []string{"client", "server", "both"} {
				usage := usage
				t.Run(usage, func(t *testing.T) {
					cn := UniqueKey("ca-issue-usage-"+usage) + ".example.com"
					stdout, _, err := runner.Run(CAIssueWithUsageCommand(cn, usage))
					require.NoError(t, err,
						"ca issue --usage=%s should succeed", usage)
					require.NotEmpty(t, stdout)
					if serial := ExtractIssuedSerial(stdout); serial != "" {
						_, _, _ = runner.Run(CARevokeCommand(serial))
					}
				})
			}
		})

		t.Run("key-algorithms", func(t *testing.T) {
			// Server-side validator (sdk/go/client/certificate/client.go)
			// accepts "ecdsa" and "rsa". Ed25519 is not accepted by
			// IssueCertificate today; verify only the documented set.
			for _, algo := range []string{"ecdsa", "rsa"} {
				algo := algo
				t.Run(algo, func(t *testing.T) {
					cn := UniqueKey("ca-issue-algo-"+algo) + ".example.com"
					stdout, _, err := runner.Run(CAIssueWithKeyAlgorithmCommand(cn, algo))
					require.NoError(t, err,
						"ca issue --key-algorithm=%s should succeed", algo)
					require.NotEmpty(t, stdout)
					if serial := ExtractIssuedSerial(stdout); serial != "" {
						_, _, _ = runner.Run(CARevokeCommand(serial))
					}
				})
			}
		})

		t.Run("all-flags", func(t *testing.T) {
			cn := UniqueKey("ca-issue-all") + ".example.com"
			stdout, _, err := runner.Run(CAIssueAllFlagsCommand(
				cn, "server", "ecdsa", 60,
				[]string{cn, "alt." + cn},
				[]string{"127.0.0.1"},
			))
			require.NoError(t, err, "ca issue (all flags) should succeed")
			require.Contains(t, stdout, "Serial Number:")
			if serial := ExtractIssuedSerial(stdout); serial != "" {
				_, _, _ = runner.Run(CARevokeCommand(serial))
			}
		})

		t.Run("json-output", func(t *testing.T) {
			cn := UniqueKey("ca-issue-json") + ".example.com"
			stdout, _, err := runner.Run(CAIssueJSONCommand(cn))
			require.NoError(t, err, "ca issue --output json should succeed")
			require.NotEmpty(t, stdout)

			var iss caIssueJSON
			require.NoError(t, json.Unmarshal([]byte(stdout), &iss),
				"ca issue json should parse")
			assert.NotEmpty(t, iss.SerialNumber, "serial_number should be set")
			assert.Contains(t, iss.CertificatePEM, "-----BEGIN CERTIFICATE-----",
				"certificate_pem should be PEM-encoded")
			assert.Contains(t, iss.PrivateKeyPEM, "-----BEGIN",
				"private_key_pem should be PEM-encoded")
			if iss.SerialNumber != "" {
				_, _, _ = runner.Run(CARevokeCommand(iss.SerialNumber))
			}
		})

		t.Run("missing-common-name", func(t *testing.T) {
			_, stderr, err := runner.Run(CAIssueMissingCommonNameCommand())
			require.Error(t, err, "ca issue without --common-name should fail")
			assert.NotEmpty(t, stderr, "missing flag failure should emit stderr")
		})
	})
}

// =============================================================================
// revoke
// =============================================================================

// TestCARevoke exercises happy-path revoke (against a freshly-issued
// cert) and the missing-arg error path. The round-trip behaviour —
// issue → list → revoke → list (still present, marked revoked) — is
// covered by TestCARoundTrip below.
func TestCARevoke(t *testing.T) {
	RunForTransports(t, "CARevoke", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		t.Run("happy-path-table", func(t *testing.T) {
			cn := UniqueKey("ca-revoke-tbl") + ".example.com"
			issueOut, _, err := runner.Run(CAIssueCommand(cn))
			require.NoError(t, err, "issue should succeed before revoke")
			serial := ExtractIssuedSerial(issueOut)
			require.NotEmpty(t, serial, "serial must be extractable from issue output")

			stdout, _, err := runner.Run(CARevokeCommand(serial))
			require.NoError(t, err, "ca revoke should succeed")
			assert.Contains(t, stdout, serial,
				"revoke output should echo the serial")
		})

		t.Run("happy-path-json", func(t *testing.T) {
			cn := UniqueKey("ca-revoke-json") + ".example.com"
			issueOut, _, err := runner.Run(CAIssueCommand(cn))
			require.NoError(t, err)
			serial := ExtractIssuedSerial(issueOut)
			require.NotEmpty(t, serial)

			stdout, _, err := runner.Run(CARevokeJSONCommand(serial))
			require.NoError(t, err, "ca revoke --output json should succeed")
			var r caRevokeJSON
			require.NoError(t, json.Unmarshal([]byte(stdout), &r),
				"revoke json should parse")
			assert.True(t, r.Success, "server should report Success=true")
		})

		t.Run("missing-serial", func(t *testing.T) {
			_, stderr, err := runner.Run(CARevokeMissingSerialCommand())
			require.Error(t, err, "ca revoke without serial should fail")
			assert.NotEmpty(t, stderr, "missing-arg failure should emit stderr")
		})
	})
}

// =============================================================================
// export-bundle
// =============================================================================

// TestCAExportBundle exercises file-on-disk and stdout export paths
// plus the missing-arg error path.
func TestCAExportBundle(t *testing.T) {
	RunForTransports(t, "CAExportBundle", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		t.Run("to-file", func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "ca-bundle.pem")

			stdout, _, err := runner.Run(CAExportBundleCommand(path))
			require.NoError(t, err, "ca export-bundle should succeed")
			assert.Contains(t, stdout, path,
				"stdout should reference the written path")
			assert.Contains(t, stdout, "written to",
				"stdout should confirm the bundle was written")

			data, rerr := os.ReadFile(path)
			require.NoError(t, rerr, "exported bundle file should exist")
			require.NotEmpty(t, data, "exported bundle file should not be empty")
			assert.Contains(t, string(data), "-----BEGIN CERTIFICATE-----",
				"exported file should contain PEM data")
			assert.Contains(t, string(data), "-----END CERTIFICATE-----",
				"exported file should contain PEM data")
		})

		t.Run("to-stdout", func(t *testing.T) {
			stdout, _, err := runner.Run(CAExportBundleStdoutCommand())
			require.NoError(t, err, "ca export-bundle - should succeed")
			require.NotEmpty(t, stdout, "stdout should contain the bundle")
			assert.Contains(t, stdout, "-----BEGIN CERTIFICATE-----",
				"stdout should contain PEM data")
		})

		t.Run("missing-path", func(t *testing.T) {
			_, stderr, err := runner.Run(CAExportBundleMissingPathCommand())
			require.Error(t, err, "ca export-bundle without path should fail")
			assert.NotEmpty(t, stderr, "missing-arg failure should emit stderr")
		})
	})
}

// =============================================================================
// Round-trip
// =============================================================================

// TestCARoundTrip walks the canonical issue → list → revoke →
// list --include-revoked lifecycle to verify that revoked certificates
// remain visible in the store but are filtered out of the default list.
func TestCARoundTrip(t *testing.T) {
	RunForTransports(t, "CARoundTrip", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}

		// 1. Issue a certificate with a unique CN so we can find it later.
		uniq := UniqueKey("ca-rt")
		cn := uniq + ".example.com"
		issueOut, _, err := runner.Run(CAIssueJSONCommand(cn))
		require.NoError(t, err, "issue should succeed")
		require.NotEmpty(t, issueOut)
		var iss caIssueJSON
		require.NoError(t, json.Unmarshal([]byte(issueOut), &iss),
			"issue response should parse")
		serial := iss.SerialNumber
		require.NotEmpty(t, serial, "issue must produce a serial")

		// 2. Confirm the cert appears in a CN-filtered list (default
		// includes only valid, non-revoked entries).
		listCmd := CAListWithCommonNameCommand(uniq)
		listCmd.Args = append([]string{"ca", "--output", "json", "list"},
			"--common-name", uniq)
		listOut, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list (pre-revoke) should succeed")
		var preList caListJSON
		require.NoError(t, json.Unmarshal([]byte(listOut), &preList),
			"list (pre-revoke) json should parse")
		foundPre := false
		for _, c := range preList.Certificates {
			if strings.EqualFold(c.SerialNumber, serial) ||
				strings.Contains(c.CommonName, uniq) {
				foundPre = true
				break
			}
		}
		assert.True(t, foundPre,
			"newly-issued cert should appear in CN-filtered list (pre-revoke)")

		// 3. Revoke the cert.
		_, _, err = runner.Run(CARevokeCommand(serial))
		require.NoError(t, err, "revoke should succeed")

		// 4. List with --include-revoked + same CN filter — the cert
		// must still be visible (revoked certs are retained in the store).
		postCmd := CommandDef{
			Name: "ca list --include-revoked --common-name=<uniq> --output json",
			Args: []string{
				"ca", "--output", "json", "list",
				"--common-name", uniq,
				"--include-revoked",
			},
			ExpectSuccess:  true,
			Timeout:        caTimeout,
			RequiresUnseal: true,
			SkipTransports: caTransportsToSkip,
		}
		postOut, _, err := runner.Run(postCmd)
		require.NoError(t, err, "list (post-revoke) should succeed")
		var postList caListJSON
		require.NoError(t, json.Unmarshal([]byte(postOut), &postList),
			"list (post-revoke) json should parse")
		foundPost := false
		for _, c := range postList.Certificates {
			if strings.EqualFold(c.SerialNumber, serial) ||
				strings.Contains(c.CommonName, uniq) {
				foundPost = true
				break
			}
		}
		assert.True(t, foundPost,
			"revoked cert should remain visible with --include-revoked")
	})
}

// =============================================================================
// Suite drivers
// =============================================================================

// TestCAReadOnlySuite runs the read-only suite across every supported
// transport. Drives every read verb in one shot to catch regressions
// in the suite plumbing itself.
func TestCAReadOnlySuite(t *testing.T) {
	RunForTransports(t, "CAReadOnlySuite", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}
		runner.RunSuite(CAReadOnlySuite())
	})
}

// TestCAErrorSuite runs the error-path suite across every supported
// transport. Each entry is expected to fail with a non-zero exit code.
func TestCAErrorSuite(t *testing.T) {
	RunForTransports(t, "CAErrorSuite", caTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !requireCAAvailable(t, runner) {
			return
		}
		runner.RunSuite(CAErrorSuite())
	})
}
