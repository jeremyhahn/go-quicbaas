package quicbaas

import (
	"strings"
	"testing"
)

// TestKeyForJobByID_LayoutIsReservedAndScoped pins the job key layout after the
// docs/18 V24 remediation.
//
// Two properties, and they are separate:
//
//   - PLATFORM RECORDS ARE IN THE RESERVED NAMESPACE. "__sys:" is excluded from
//     go-qrdb's user-facing List and Scan and refused by MVCC restore, so job
//     records — which carry handler names and payloads — cannot be enumerated by
//     an ordinary tenant sweep. Bare "sys:" bought none of that; asserting the
//     literal prefix is what stops it drifting back.
//   - A TENANT'S RECORDS STAY IN THE TENANT'S NAMESPACE. They are that tenant's
//     to enumerate, and hiding them in the reserved namespace would take that
//     away for no benefit.
//
// The pin also guards the property the earlier version existed for: scopeToken
// is shared between job identity and the claim key, so this proves a change to
// one did not silently move the other. That guard survives the rename even
// though the namespace question split away into recordNamespace.
func TestKeyForJobByID_LayoutIsReservedAndScoped(t *testing.T) {
	if got, want := keyForJobByID(7, ""), "__sys:scheduler/jobs/7"; got != want {
		t.Errorf("system job key = %q, want %q", got, want)
	}
	if got, want := keyForJobByID(7, "acme"), "t:acme:scheduler/jobs/7"; got != want {
		t.Errorf("tenant job key = %q, want %q", got, want)
	}
	if got, want := keyForJobByID(7, ""), SystemJobsPrefix+"7"; got != want {
		t.Errorf("system job key %q must still match SystemJobsPrefix %q", got, want)
	}
}

// TestPlatformPrefixes_AreAllReserved sweeps every platform-owned prefix this
// package writes, so a prefix added later cannot quietly reintroduce V24.
func TestPlatformPrefixes_AreAllReserved(t *testing.T) {
	prefixes := map[string]string{
		"jobs":       SystemJobsPrefix,
		"executions": ExecutionsPrefix,
		"counter":    JobIDCounterKey,
		"claims":     ClaimsPrefix,
		"tenantjobs": TenantJobIndexPrefix,
	}
	for name, prefix := range prefixes {
		if !strings.HasPrefix(prefix, systemNamespace) {
			t.Errorf("%s prefix %q is not in the reserved namespace — platform records "+
				"would be enumerable by a tenant scan", name, prefix)
		}
	}
}

// TestScopeTokenAndNamespace_AnswerDifferentQuestions pins the split that the
// V24 rename forced.
//
// scopeToken says which scope owns an ID and is embedded inside a compound key;
// recordNamespace says where a top-level record lives. Collapsing them is the
// obvious first pass and it produces "__sys:scheduler/claims/__sys:7" — the
// namespace nested inside itself. Both must stay scope-unique, which is the
// property V4 was actually about.
func TestScopeTokenAndNamespace_AnswerDifferentQuestions(t *testing.T) {
	if scopeToken("") == recordNamespace("") {
		t.Error("the system scope token and the system namespace must differ, or " +
			"the namespace ends up nested inside compound keys as a discriminator")
	}
	// Tenants happen to share a spelling; what matters is that each stays unique.
	if scopeToken("acme") == scopeToken("globex") {
		t.Error("two tenants must not share a scope token")
	}
	if recordNamespace("acme") == recordNamespace("") {
		t.Error("a tenant's namespace must differ from the platform's")
	}
}
