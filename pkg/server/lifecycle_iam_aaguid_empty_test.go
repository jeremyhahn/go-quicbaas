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
// An allowlist file that admits nothing is a mistake, not a policy.
//
// The decision, made deliberately because ambiguity is how this became a
// silent deny-all: an allowlist document yielding no usable entry is a
// CONFIGURATION ERROR, refused at load with the file named.
//
// "Allow nothing" needs no file to express — an unconfigured allowlist already
// denies every enrollment that must prove hardware. So an empty file cannot
// express anything a deployment could want, and can only be a truncated
// download, an unfilled template, or a filter that matched nothing. Accepting
// it produced a node that started, reported healthy, and then refused every
// WebAuthn enrollment with a message about the authenticator rather than about
// the file — the operator debugs the gate instead of the typo.
//
// The MDS3 branch already refused this. The hand-written array branch did not,
// which is the inconsistency fixed here.
// =============================================================================

// writeAllowlist writes an allowlist document and returns its path.
func writeAllowlist(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// TestBuildAAGUIDAllowlist_RefusesADocumentThatAdmitsNothing covers every shape
// an operator can produce that parses but grants nothing.
func TestBuildAAGUIDAllowlist_RefusesADocumentThatAdmitsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty array", `[]`},
		{"entries without an aaguid", `[{"name": "YubiKey 5"}, {"name": "SoloKey"}]`},
		{"entries with empty aaguid strings", `[{"aaguid": "", "name": "YubiKey 5"}]`},
		{"MDS3 document with no admissible entry", `{"entries": []}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeAllowlist(t, "allowlist.json", tc.body)

			allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
				Mode:          "enforce",
				AllowlistFile: path,
			})

			assert.Nil(t, allowlist, "a refusal must produce no allowlist")
			require.Error(t, err, "a document admitting nothing was accepted, so every "+
				"WebAuthn enrollment will be refused by a gate that looks configured")

			var parseErr *AAGUIDAllowlistParseError
			require.ErrorAs(t, err, &parseErr)
			assert.Equal(t, path, parseErr.Path,
				"the refusal must name the file the operator has to fix")
			assert.Contains(t, err.Error(), "aaguid",
				"the refusal must say what the document was missing")
		})
	}
}

// TestBuildAAGUIDAllowlist_AcceptsADocumentWithUsableEntries is the positive
// control, and the half that matters: a check that refused every file would
// satisfy the test above while breaking every real deployment.
func TestBuildAAGUIDAllowlist_AcceptsADocumentWithUsableEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{
			name: "hand-written array",
			body: `[{"aaguid": "cb69481e8ff7403993ec0a2729a154a8", "name": "YubiKey 5"}]`,
			want: 1,
		},
		{
			// A partially-filled file still configures the entries it does
			// carry: refusing here would punish an operator for a comment-like
			// placeholder alongside real entries.
			name: "array mixing usable and unusable entries",
			body: `[{"name": "placeholder"}, {"aaguid": "cb69481e8ff7403993ec0a2729a154a8"}]`,
			want: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeAllowlist(t, "allowlist.json", tc.body)

			allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{
				Mode:          "enforce",
				AllowlistFile: path,
			})

			require.NoError(t, err, "a usable allowlist was refused")
			require.NotNil(t, allowlist)
			assert.Equal(t, iam.AAGUIDPolicyStrict, allowlist.Policy())
			assert.Equal(t, tc.want, allowlist.Len())
		})
	}
}

// TestBuildAAGUIDAllowlist_NoFileStillMeansAllowNothing records why refusing an
// empty file loses no capability: the deployment that wants to admit nothing
// already gets it by configuring no file at all.
func TestBuildAAGUIDAllowlist_NoFileStillMeansAllowNothing(t *testing.T) {
	allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{Mode: "enforce"})

	require.NoError(t, err)
	require.NotNil(t, allowlist)
	assert.Equal(t, 0, allowlist.Len(),
		"an unconfigured allowlist must still be the empty, deny-everything one")
}
