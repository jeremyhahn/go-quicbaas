//go:build rest

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

package rest

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// This file keeps the REST package's TWO authorization tables from diverging.
//
// There are two:
//
//	pepMapping + pepSpecialRules (authz_pep_mapping.go) — consulted by
//	  AuthorizeViaValidator, the gate the production posture installs
//	  (SecurityConfig.RequireAuthz defaults to true). It is default-DENY: an
//	  unmapped protected path is 403 with no validator call at all.
//
//	defaultDenyScopeTable + barrierScopeTable (authz_default_deny.go) —
//	  consulted by EnforceDefaultDeny, which is reachable only through
//	  SetDefaultDenyEnforcement. That setter has no production caller today, so
//	  this table is exercised only by tests. It is default-ALLOW-with-token: an
//	  unmapped path requires authentication but no capability.
//
// The two tables silently drifting is what produced the "mounted but always
// 403" class of bug: a surface was listed in the default-deny table (so it
// looked gated) while the live PEP had no entry for it at all. The invariant
// asserted here is the one that matters: EVERY surface either table names must
// resolve through the live PEP. Scope GRANULARITY may still differ — the
// default-deny rules are deliberately coarse where a literal prefix cannot
// express a variable segment, and a coarse rule is never weaker than the
// granular one it stands in for — but COVERAGE may not.
//
// The right long-term fix is to delete the second table outright (see
// TestDefaultDennyTableHasNoProductionCaller for the evidence that it is dead)
// and derive requiredScopeForRequest from the PEP. Until that happens, these
// tests make a repeat of the divergence a build failure.

// agreementProbeMethods are the verbs probed against each table prefix: one
// read, one create-shaped write, one delete.
var agreementProbeMethods = []string{http.MethodGet, http.MethodPost, http.MethodDelete}

// scopeTablePrefixes returns every path prefix named by the default-deny gate's
// tables, sorted for deterministic output.
func scopeTablePrefixes() []string {
	prefixes := make([]string, 0, len(defaultDenyScopeTable)+len(barrierScopeTable))
	for prefix := range defaultDenyScopeTable {
		prefixes = append(prefixes, prefix)
	}
	for prefix := range barrierScopeTable {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return prefixes
}

// TestAuthzTablesAgreeOnCoverage asserts that every surface the default-deny
// scope tables gate also resolves through the live validator PEP. A prefix
// present in one table and absent from the other is exactly the divergence that
// made Trust, ACME, the xkms compatibility API, the secret side-mounts, the
// legacy seal API, /api/v1/batch and /api/v1/query unreachable in production.
func TestAuthzTablesAgreeOnCoverage(t *testing.T) {
	var missing []string
	for _, prefix := range scopeTablePrefixes() {
		for _, method := range agreementProbeMethods {
			// Probe the collection shape and the item shape: the PEP
			// distinguishes them and a mapping could cover only one.
			for _, path := range []string{prefix, prefix + "/agreement-probe"} {
				if _, _, ok := mapRequestToPermission(path, method); !ok {
					missing = append(missing, method+" "+path)
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d path(s) are gated by the default-deny scope table but UNMAPPED in "+
			"the live validator PEP (hard-403 in production):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestAuthzTablesAgreeOnSharedGranularSurfaces asserts EXACT scope agreement for
// the surfaces where both gates carry per-operation tables. These share a single
// source of truth (barrierGranularOps and legacySealOps, both owned by
// authz_pep_mapping.go), so any divergence here means somebody re-forked a table.
func TestAuthzTablesAgreeOnSharedGranularSurfaces(t *testing.T) {
	type probe struct {
		path   string
		method string
	}
	probes := make([]probe, 0, len(barrierGranularOps)*len(barrierPrefixes)+len(legacySealOps))
	for _, prefix := range barrierPrefixes {
		for _, op := range barrierGranularOps {
			probes = append(probes, probe{prefix + op.suffix, op.method})
		}
	}
	for _, op := range legacySealOps {
		probes = append(probes, probe{legacySealPrefix + op.suffix, op.method})
	}

	for _, p := range probes {
		resource, action, ok := mapRequestToPermission(p.path, p.method)
		if !ok {
			t.Errorf("%s %s: unmapped in the validator PEP", p.method, p.path)
			continue
		}
		if got, want := resource+":"+action, requiredScopeForRequest(p.path, p.method); got != want {
			t.Errorf("%s %s: PEP scope %q != default-deny scope %q", p.method, p.path, got, want)
		}
	}
}

// TestAuthzTablesAgreeOnPublicSurface asserts the two pre-auth allowlists agree
// on every path either of them names, so a route cannot be public under one
// posture and gated under the other.
func TestAuthzTablesAgreeOnPublicSurface(t *testing.T) {
	// publicEndpoints is the narrower list by design: the per-group gates never
	// see the root-mounted pre-token surfaces (OIDC discovery, /oauth2, ACME
	// directory, invites, UI, docs) that only the top-router default-deny gate
	// has to admit, and /api/v1/metrics is deliberately gated under the
	// per-group postures. Every entry it DOES name must be public under
	// default-deny too, otherwise a caller that can reach an endpoint pre-auth
	// today would be locked out by flipping the posture.
	for _, endpoint := range publicEndpoints {
		if !isDefaultDenyPublic(endpoint) {
			t.Errorf("%q is public for the per-group gates but NOT under default-deny", endpoint)
		}
	}
}

// TestDefaultDenyTableHasNoProductionCaller documents, as an executable
// assertion, why the second table is safe to delete: the ONLY way to install
// EnforceDefaultDeny is SetDefaultDenyEnforcement, and no production code path
// calls it. If a production caller is ever added this test's premise changes and
// the comment block at the top of this file must be revisited.
//
// The assertion itself is behavioural: a server built the normal way never has
// default-deny enforcement active, so the gate — and the table behind it — is
// unreachable outside tests.
func TestDefaultDenyTableHasNoProductionCaller(t *testing.T) {
	s, _ := newTestServer(t)
	if s.defaultDenyEnforcement {
		t.Fatal("a normally constructed server has default-deny enforcement active; " +
			"the second scope table is no longer dead and the tables must be unified")
	}
}
