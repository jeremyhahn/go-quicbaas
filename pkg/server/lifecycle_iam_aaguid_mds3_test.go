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

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// The documented allowlist format has to be one the parser can read.
//
// Two doc comments — pkg/config's AllowlistFile field and aaguidEntry's own —
// promise that iam.aaguid.allowlist_file accepts "the FIDO Alliance MDS3
// format or a simple JSON array". The parser only ever read the array.
//
// While a build failure merely disabled enforcement that mismatch was a silent
// fail-open. Now that it is fatal, it is a deployment that will not boot on a
// file the documentation told the operator to use.
// =============================================================================

// mds3Payload is the decoded FIDO Metadata Service v3 blob, trimmed to the
// shape an operator's file actually has: a top-level object with a metadata
// header and an entries array, each entry carrying its AAGUID at the top level,
// its human-readable name inside metadataStatement, and its certification
// history in statusReports.
//
// The Yubico entry is current; the second entry is REVOKED, which is the case
// that matters — importing a metadata blob wholesale would allowlist an
// authenticator the FIDO Alliance has withdrawn.
const mds3Payload = `{
  "legalHeader": "Retrieval and use of this BLOB indicates acceptance of the FIDO Alliance Metadata Service terms.",
  "no": 76,
  "nextUpdate": "2026-10-01",
  "entries": [
    {
      "aaguid": "cb69481e-8ff7-4039-93ec-0a2729a154a8",
      "metadataStatement": {
        "description": "YubiKey 5 Series",
        "authenticatorVersion": 2,
        "protocolFamily": "fido2"
      },
      "statusReports": [
        {"status": "FIDO_CERTIFIED", "effectiveDate": "2020-05-12"},
        {"status": "FIDO_CERTIFIED_L1", "effectiveDate": "2020-11-19"}
      ],
      "timeOfLastStatusChange": "2020-11-19"
    },
    {
      "aaguid": "d8522d9f-575b-4866-88a9-ba99fa02f35b",
      "metadataStatement": {
        "description": "Withdrawn Authenticator",
        "protocolFamily": "fido2"
      },
      "statusReports": [
        {"status": "FIDO_CERTIFIED", "effectiveDate": "2019-01-01"},
        {"status": "REVOKED", "effectiveDate": "2021-06-02"}
      ],
      "timeOfLastStatusChange": "2021-06-02"
    }
  ]
}`

// writeAllowlistFile writes raw JSON to a temp allowlist file and returns its
// path.
func writeAllowlistFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aaguid-allowlist.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// TestBuildAAGUIDAllowlist_AcceptsMDS3Document is the finding: the documented
// MDS3 shape must load, not abort the boot.
func TestBuildAAGUIDAllowlist_AcceptsMDS3Document(t *testing.T) {
	allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
		AllowlistFile: writeAllowlistFile(t, mds3Payload),
	})
	require.NoError(t, err, "the documented MDS3 format was rejected")
	require.NotNil(t, allowlist)

	// The current authenticator is admitted. AAGUIDs arrive dashed in MDS3 and
	// undashed on the wire; the allowlist normalises both.
	yubiKey5 := []byte{0xcb, 0x69, 0x48, 0x1e, 0x8f, 0xf7, 0x40, 0x39,
		0x93, 0xec, 0x0a, 0x27, 0x29, 0xa1, 0x54, 0xa8}
	assert.NoError(t, allowlist.Check(yubiKey5),
		"a currently certified MDS3 authenticator was not admitted")
}

// TestBuildAAGUIDAllowlist_MDS3RevokedEntryIsNotAllowlisted is the half that
// makes reading MDS3 safe rather than merely convenient.
//
// An entry's statusReports carry its certification history, and the blob
// includes authenticators the Alliance has REVOKED or found compromised.
// Importing every entry would hand an operator an allowlist that trusts them —
// a fail-open introduced by a convenience feature, which is the same defect
// class this PR exists to remove.
func TestBuildAAGUIDAllowlist_MDS3RevokedEntryIsNotAllowlisted(t *testing.T) {
	allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
		AllowlistFile: writeAllowlistFile(t, mds3Payload),
	})
	require.NoError(t, err)

	revoked := []byte{0xd8, 0x52, 0x2d, 0x9f, 0x57, 0x5b, 0x48, 0x66,
		0x88, 0xa9, 0xba, 0x99, 0xfa, 0x02, 0xf3, 0x5b}
	assert.Error(t, allowlist.Check(revoked),
		"a REVOKED MDS3 entry was imported into the allowlist")
}

// TestBuildAAGUIDAllowlist_StillAcceptsTheArrayFormat is the control for the
// other documented shape: adding MDS3 must not cost the simple one.
func TestBuildAAGUIDAllowlist_StillAcceptsTheArrayFormat(t *testing.T) {
	allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
		AllowlistFile: writeAllowlistFile(t,
			`[{"aaguid":"cb69481e8ff7403993ec0a2729a154a8","name":"YubiKey 5 Series"}]`),
	})
	require.NoError(t, err)

	yubiKey5 := []byte{0xcb, 0x69, 0x48, 0x1e, 0x8f, 0xf7, 0x40, 0x39,
		0x93, 0xec, 0x0a, 0x27, 0x29, 0xa1, 0x54, 0xa8}
	assert.NoError(t, allowlist.Check(yubiKey5))
}

// TestBuildAAGUIDAllowlist_StillFailsClosedOnUnreadableInput proves the fix
// does not reopen what the hardening closed. Accepting a second shape is not a
// licence to guess: anything that is neither documented shape, and any object
// that carries no usable entries, must still refuse the boot rather than yield
// a silently empty allowlist.
func TestBuildAAGUIDAllowlist_StillFailsClosedOnUnreadableInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"not json at all", `{not json`},
		{"truncated mds3", `{"entries": [{"aaguid": "cb69481e`},
		{"a bare string", `"cb69481e8ff7403993ec0a2729a154a8"`},
		{"an object that is not mds3", `{"authenticators": ["cb69481e8ff7403993ec0a2729a154a8"]}`},
		{"mds3 with no entries", `{"legalHeader": "x", "no": 1, "entries": []}`},
		{"array of the wrong shape", `[{"aaguid": 12345}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
				AllowlistFile: writeAllowlistFile(t, tc.body),
			})
			require.Error(t, err, "an unreadable allowlist booted the node")
			assert.Nil(t, allowlist, "a refusal must yield no allowlist")

			var parseErr *AAGUIDAllowlistParseError
			require.ErrorAs(t, err, &parseErr)
			assert.Contains(t, parseErr.Error(), "aaguid",
				"the refusal must name the setting the operator has to fix")
		})
	}
}

// TestBuildAAGUIDAllowlist_ParseFailureNamesBothShapes keeps the refusal
// actionable. An operator who pointed the setting at the wrong file needs to be
// told what the parser accepts, not just that it did not like the input.
func TestBuildAAGUIDAllowlist_ParseFailureNamesBothShapes(t *testing.T) {
	_, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
		AllowlistFile: writeAllowlistFile(t, `"just a string"`),
	})
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "entries", "the message does not mention the MDS3 shape")
	assert.Contains(t, msg, "aaguid", "the message does not mention the array shape")
}

// TestBuildAAGUIDAllowlist_MDS3EntryWithoutAAGUIDIsSkipped covers the U2F and
// UAF entries a real blob carries: they identify themselves by
// attestationCertificateKeyIdentifiers rather than an AAGUID, so there is
// nothing for a FIDO2 allowlist to hold. Skipping them must not fail the load —
// every real MDS3 blob contains some.
func TestBuildAAGUIDAllowlist_MDS3EntryWithoutAAGUIDIsSkipped(t *testing.T) {
	body := `{
      "entries": [
        {"attestationCertificateKeyIdentifiers": ["7b3c"], "statusReports": [{"status": "FIDO_CERTIFIED"}]},
        {"aaguid": "cb69481e-8ff7-4039-93ec-0a2729a154a8", "statusReports": [{"status": "FIDO_CERTIFIED"}]}
      ]
    }`
	allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
		AllowlistFile: writeAllowlistFile(t, body),
	})
	require.NoError(t, err, "a U2F entry with no AAGUID aborted the load")
	assert.Equal(t, 1, allowlist.Len())
	assert.Equal(t, iam.AAGUIDPolicyStrict, allowlist.Policy())
}
