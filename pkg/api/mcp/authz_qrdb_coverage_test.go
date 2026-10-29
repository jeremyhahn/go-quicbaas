//go:build mcp

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

package mcp

import (
	"os"
	"sort"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// registeredToolNames returns every tool name the MCP surface registers, from
// both registries the server exposes: the main ToolRegistry and the standalone
// vector MCPServer. Together they are the complete set a client can ever see
// in tools/list.
func registeredToolNames(t *testing.T) []string {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	seen := make(map[string]struct{})
	for _, tool := range NewToolRegistry(&agentMockQuicBaaS{}, log).ListTools() {
		seen[tool.Name] = struct{}{}
	}
	for _, tool := range NewMCPServer(nil, "", log).GetTools() {
		seen[tool.Name] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestMCPPermissions_EveryToolMappedOrPublic is the registry-exhaustive guard
// for the MCP surface, modelled on the gRPC descriptor guard
// TestGRPCPermissions_EveryMethodMapped (pkg/api/grpc/authz_interceptor_test.go).
//
// The MCP authz gate is default-deny: a tool with no qrdb permission mapping is
// denied AND filtered out of tools/list by FilterToolsByAuthorization, so it is
// invisible to every MCP client. Registering a tool without a mapping ships a
// DEAD tool, silently. That is exactly how the six base certificate tools
// (quicbaas_certificate_issue / sign_csr / revoke / verify / get_ca /
// get_tls_config) stayed invisible while the three certificate EXTENSION tools
// added beside them worked.
//
// Every registered tool must therefore be in exactly one of three buckets:
//
//  1. public — publicTools; no token, no permission;
//  2. mapped — getToolQRDBPermission resolves a qrdb permission;
//  3. declared dead — listed in unmappedToolGroups with a reason and a
//     resolution (authz_qrdb_unmapped.go).
//
// A newly registered tool lands in none of them and fails here.
func TestMCPPermissions_EveryToolMappedOrPublic(t *testing.T) {
	t.Parallel()

	for _, name := range registeredToolNames(t) {
		_, _, mapped := getToolQRDBPermission(name)
		public := IsPublicTool(name)
		declaredDead := isDeclaredUnmappedTool(name)

		if !mapped && !public && !declaredDead {
			t.Errorf("MCP tool %q is neither mapped in qrdbToolExactMapping/"+
				"qrdbToolPrefixMapping, nor declared public in publicTools, nor "+
				"declared deliberately dead in unmappedToolGroups. The authz gate is "+
				"default-deny, so as registered it is invisible in tools/list: add a "+
				"permission mapping (matching the REST and gRPC rules for the same "+
				"verb), or declare it public, or declare it dead with a reason.", name)
		}
	}
}

// TestMCPPermissions_BucketsAreDisjoint asserts a tool never lands in two
// buckets. A tool that is both public and mapped has an unenforced permission;
// a tool that is both mapped and declared dead has stale documentation that
// would hide the next real regression.
func TestMCPPermissions_BucketsAreDisjoint(t *testing.T) {
	t.Parallel()

	for _, name := range registeredToolNames(t) {
		_, _, mapped := getToolQRDBPermission(name)
		public := IsPublicTool(name)
		declaredDead := isDeclaredUnmappedTool(name)

		assert.Falsef(t, public && mapped,
			"tool %q is public AND mapped; the mapping is never enforced", name)
		assert.Falsef(t, public && declaredDead,
			"tool %q is public AND declared dead; it is reachable, so remove it from unmappedToolGroups", name)
		assert.Falsef(t, mapped && declaredDead,
			"tool %q is mapped AND declared dead; remove the stale unmappedToolGroups entry", name)
	}
}

// TestMCPPermissions_DeclaredDeadToolsExist is the reverse guard: every name in
// unmappedToolGroups must belong to a registered tool. A rename that leaves a
// stale entry behind would let the renamed tool ship dead unnoticed, which is
// the failure mode the forward guard exists to catch.
func TestMCPPermissions_DeclaredDeadToolsExist(t *testing.T) {
	t.Parallel()

	registered := make(map[string]struct{})
	for _, name := range registeredToolNames(t) {
		registered[name] = struct{}{}
	}

	for _, group := range unmappedToolGroups {
		assert.NotEmptyf(t, group.reason, "unmappedToolGroups entry %v has no reason", group.tools)
		assert.NotEmptyf(t, group.resolution, "unmappedToolGroups entry %v has no resolution", group.tools)
		for _, name := range group.tools {
			_, ok := registered[name]
			assert.Truef(t, ok,
				"unmappedToolGroups declares %q dead but no such tool is registered; "+
					"remove the stale entry or fix the name", name)
		}
	}
}

// TestMCPPermissions_EveryMappingIsRegistered asserts every permission the
// mapping tables require actually exists in the permission registry the qrdb
// policy decision point resolves against.
//
// This is the other half of the dead-tool problem: a tool mapped onto an
// unregistered permission is just as unreachable as an unmapped one, because
// go-qrdb's role store rejects an unregistered permission on role
// create/update — so no role can hold the grant — but it LOOKS wired, so it
// survives review.
//
// The registry has two contributors: go-qrdb's own init()-registered families,
// and the broker-domain families QuicBaaS contributes through
// qrdbsdk.RegisterPermissions (pkg/permission, called from server.NewServer).
// The test registers the broker families explicitly rather than importing
// pkg/server, which would be an import cycle; quicbaaspermission.Register is
// idempotent. There is no exception list: a mapping onto a permission neither
// side registers is a bug, not a documented gap.
func TestMCPPermissions_EveryMappingIsRegistered(t *testing.T) {
	t.Parallel()

	quicbaaspermission.Register()

	registered := make(map[string]struct{})
	for _, perm := range qrdbsdk.AllPermissions() {
		registered[string(perm)] = struct{}{}
	}
	require.NotEmpty(t, registered, "qrdb permission registry is empty")

	check := func(tool string, m qrdbToolResource) {
		t.Helper()
		_, ok := registered[m.resource+":"+m.action]
		assert.Truef(t, ok,
			"tool %q maps to %s:%s, which neither go-qrdb nor pkg/permission registers; "+
				"no role can hold that grant, so the tool is unreachable despite looking "+
				"wired. Register the permission in pkg/permission or fix the mapping.",
			tool, m.resource, m.action)
	}

	for tool, m := range qrdbToolExactMapping {
		check(tool, m)
	}
	for prefix, m := range qrdbToolPrefixMapping {
		check(prefix+"*", m)
	}
}

// TestMCPPermissions_EveryMappedToolIsRegistered is the reverse of
// TestMCPPermissions_EveryToolMappedOrPublic: every NAME in qrdbToolExactMapping
// must belong to a tool that something registers.
//
// This is the invariant whose absence let issue #905's dead surface survive. A
// permission entry for a tool nobody registers is invisible in both directions —
// the forward guard iterates registered tools and never sees it, and a reader of
// the table cannot tell a live mapping from a name that was renamed, never
// implemented, or deleted years ago. Fourteen such entries were found when the
// guard was written: eight for the quicbaas_user_* / quicbaas_role_* families
// that were never registered as tools, and six KV aliases (quicbaas_get,
// quicbaas_put, quicbaas_set, quicbaas_scan, quicbaas_list, quicbaas_delete)
// that named no tool and no dispatch case.
//
// "Registers" has to be measured across BUILD CONFIGURATIONS, not just this one.
// authz_qrdb.go carries no build tag, so it declares permissions for tools whose
// registration IS tagged — quicbaas_rag_* exists only under `rag`, and in the
// `make test-lean` pass (transports on, rag off) the compiled registry does not
// contain it. Judging deadness from the compiled registry alone therefore calls
// every feature-gated tool dead in the pass that disables its feature, which is
// how the first version of this test went red on test-lean while passing the
// maximal pass.
//
// So a name is accepted when EITHER the compiled registry has it (covers names
// built at runtime from a prefix) OR the literal appears in some non-test source
// file of this package other than the mapping tables themselves (covers names
// whose registration sits behind a build tag this pass turned off). A name that
// satisfies neither appears NOWHERE but the permission table: nothing registers
// it and nothing dispatches it, in any configuration.
//
// There is deliberately no allowlist. Prefix mappings are exempt by
// construction: a prefix matches a family, not a name, and the forward guard
// already proves every registered tool a prefix serves resolves through it.
func TestMCPPermissions_EveryMappedToolIsRegistered(t *testing.T) {
	t.Parallel()

	registered := make(map[string]struct{})
	for _, name := range registeredToolNames(t) {
		registered[name] = struct{}{}
	}
	require.NotEmpty(t, registered, "no MCP tools registered")

	sources := packageSourcesExcludingMappingTables(t)

	for tool := range qrdbToolExactMapping {
		if _, ok := registered[tool]; ok {
			continue
		}
		literal := `"` + tool + `"`
		found := false
		for file, body := range sources {
			if strings.Contains(body, literal) {
				found = true
				t.Logf("tool %q is not registered in this build configuration but is "+
					"named in %s, so its registration is build-tagged", tool, file)
				break
			}
		}
		assert.Truef(t, found,
			"qrdbToolExactMapping has an entry for %q, but no registry registers a tool "+
				"by that name and the name appears in no source file of this package "+
				"outside the mapping tables. The entry is dead in every build "+
				"configuration: it grants nothing and documents nothing. Register the "+
				"tool or delete the entry.", tool)
	}
}

// packageSourcesExcludingMappingTables reads every non-test Go source file of
// this package EXCEPT the permission-mapping tables, so that an entry cannot
// vouch for itself. Build tags are ignored on purpose: the point is to see the
// registrations this build configuration excluded.
func packageSourcesExcludingMappingTables(t *testing.T) map[string]string {
	t.Helper()

	selfReferential := map[string]struct{}{
		"authz_qrdb.go":          {},
		"authz_qrdb_unmapped.go": {},
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	sources := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, skip := selfReferential[name]; skip {
			continue
		}
		body, err := os.ReadFile(name)
		require.NoErrorf(t, err, "reading %s", name)
		sources[name] = string(body)
	}
	require.NotEmpty(t, sources, "no package sources found")
	return sources
}

// TestMCPPermissions_CertificateToolsMatchGRPC pins the six base certificate
// tools to the exact (resource, action) pairs
// quicbaas.certificate.v1.CertificateService requires on gRPC
// (pkg/api/grpc/authz_mapping.go) and /api/v1/certificate requires on REST
// (pkg/api/rest/authz_pep_mapping.go).
//
// It also pins the read/write split: the three read verbs must never acquire a
// write action, and the three write verbs must never be downgraded to a read.
func TestMCPPermissions_CertificateToolsMatchGRPC(t *testing.T) {
	t.Parallel()

	expected := map[string]qrdbToolResource{
		// Writes: issuance and CSR signing mint a leaf; revoke drives the CRL.
		"quicbaas_certificate_issue":    {"certs", "generate"},
		"quicbaas_certificate_sign_csr": {"certs", "generate"},
		"quicbaas_certificate_revoke":   {"certs", "revoke"},
		// Reads.
		"quicbaas_certificate_verify":         {"certs", "read"},
		"quicbaas_certificate_get_ca":         {"certs", "read"},
		"quicbaas_certificate_get_tls_config": {"certs", "read"},
		// The certificate EXTENSION tools, pinned alongside so the two halves
		// of the surface cannot drift apart again.
		"quicbaas_certificate_cross_sign": {"ca", "create"},
		// The chain walk imports issuer certificates into the server CA's
		// stores, which every tenant shares: the cross-tenant aia:import.
		"quicbaas_certificate_chain":                 {"aia", "import"},
		"quicbaas_certificate_issue_device_identity": {"certs", "generate"},
	}

	writeActions := map[string]bool{"generate": true, "revoke": true, "create": true, "import": true}

	for tool, want := range expected {
		resource, action, ok := getToolQRDBPermission(tool)
		require.Truef(t, ok, "certificate tool %q has no qrdb permission mapping", tool)
		assert.Equalf(t, want.resource, resource, "tool %q resource", tool)
		assert.Equalf(t, want.action, action, "tool %q action", tool)
		assert.Equalf(t, writeActions[want.action], writeActions[action],
			"tool %q crossed the read/write boundary", tool)
	}
}
