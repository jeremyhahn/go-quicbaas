package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// storedPolicyJSON is a policy record as an older build wrote it: the JSON
// encoding of an iam.Policy put straight into the store, never through the
// strict YAML parse.
const storedPolicyJSON = `{
  "apiVersion": "iam.quicbaas.io/v1",
  "kind": "Policy",
  "metadata": {"name": "stored-eng", "tenant": "t-stored", "created": "2026-01-02T03:04:05Z"},
  "spec": {
    "subjects": {"authenticated": true},
    "rules": [
      {
        "name": "eng-read",
        "effect": "allow",
        "actions": ["read"],
        "resources": ["docs/*"],
        "when": [{"expression": "subject.attributes.dept == 'eng'"}],
        "unless": [{"expression": "subject.attributes.level == 'guest'"}]
      }
    ]
  }
}`

// A policy stored before the strict YAML parse loads from the store and the
// qrdb ABAC evaluator applies its when and unless conditions.
func TestStoredPolicy_LoadsAndEvaluates(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	if err := kv.Put(ctx, iam.KeyPrefixTenant+"t-stored"+iam.KeyPrefixPolicy+"stored-eng", []byte(storedPolicyJSON)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:   iam.NewBackendStorage(kv),
		Simulator: newQRDBPolicySimulator(),
	})

	stored, err := engine.GetPolicy(ctx, "t-stored", "stored-eng")
	if err != nil {
		t.Fatalf("GetPolicy() error = %v", err)
	}
	if _, err := policyToABAC(stored); err != nil {
		t.Fatalf("policyToABAC() error = %v; the ABAC sync would refuse the stored policy", err)
	}

	// The same policy built in Go: the stored record must decide exactly as
	// this literal does, so any difference is the load path's doing. go-qrdb
	// resolves custom subject attributes only under subject.attributes.<key>.
	literal := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: "stored-eng", Tenant: "t-stored"},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{Authenticated: true},
			Rules: []iam.Rule{{
				Name:      "eng-read",
				Effect:    iam.EffectAllow,
				Actions:   []string{"read"},
				Resources: []string{"docs/*"},
				When:      []iam.Condition{{Expression: "subject.attributes.dept == 'eng'"}},
				Unless:    []iam.Condition{{Expression: "subject.attributes.level == 'guest'"}},
			}},
		},
	}

	tests := []struct {
		name  string
		attrs map[string]interface{}
		want  bool
	}{
		{name: "when holds", attrs: map[string]interface{}{"dept": "eng"}, want: true},
		{name: "when fails", attrs: map[string]interface{}{"dept": "ops"}, want: false},
		{name: "unless holds", attrs: map[string]interface{}{"dept": "eng", "level": "guest"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := &iam.CheckRequest{
				Subject:  iam.Subject{ID: "alice", TenantID: "t-stored", Attributes: tt.attrs},
				Action:   "read",
				Resource: "docs/plan",
			}
			fromStore, err := engine.SimulatePolicy(ctx, &iam.PolicySimulationRequest{Policy: stored, CheckRequest: check})
			if err != nil {
				t.Fatalf("SimulatePolicy(stored) error = %v", err)
			}
			fromLiteral, err := engine.SimulatePolicy(ctx, &iam.PolicySimulationRequest{Policy: literal, CheckRequest: check})
			if err != nil {
				t.Fatalf("SimulatePolicy(literal) error = %v", err)
			}
			if fromStore.Allowed != fromLiteral.Allowed {
				t.Fatalf("stored allowed = %v (reason %q), literal allowed = %v (reason %q); want the same decision",
					fromStore.Allowed, fromStore.Reason, fromLiteral.Allowed, fromLiteral.Reason)
			}
			if fromStore.Allowed != tt.want {
				t.Fatalf("SimulatePolicy() allowed = %v, want %v (reason %q)", fromStore.Allowed, tt.want, fromStore.Reason)
			}
		})
	}
}

// storedPolicyWarnLog records the WARN lines the ABAC sync emits.
type storedPolicyWarnLog struct{ lines []string }

func (l *storedPolicyWarnLog) Warnf(format string, args ...interface{}) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

// TestStoredPolicy_FlaggedRecordDenies pins the fail-closed evaluation: a
// stored record with keys the strict decoder refuses grants nothing. The
// simulator denies a request its rules would have allowed, and the ABAC sync
// never hands it to qrdb and logs it at WARN, while the good record next to
// it is still converted.
func TestStoredPolicy_FlaggedRecordDenies(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	prefix := iam.KeyPrefixTenant + "t-stored" + iam.KeyPrefixPolicy
	if err := kv.Put(ctx, prefix+"stored-eng", []byte(storedPolicyJSON)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	unknownKey := strings.Replace(storedPolicyJSON, `"name": "stored-eng"`, `"name": "stored-bad", "owner": "ops"`, 1)
	if err := kv.Put(ctx, prefix+"stored-bad", []byte(unknownKey)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:   iam.NewBackendStorage(kv),
		Simulator: newQRDBPolicySimulator(),
	})

	listed, err := engine.ListPolicies(ctx, "t-stored", iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListPolicies() error = %v", err)
	}
	var flagged *iam.Policy
	for _, p := range listed {
		if p.Metadata.Name == "stored-bad" {
			flagged = p
		}
	}
	if len(listed) != 2 || flagged == nil || flagged.LoadError == nil {
		t.Fatalf("ListPolicies() = %d entries, flagged = %+v; want the good record and a flagged stored-bad", len(listed), flagged)
	}

	check := &iam.CheckRequest{
		Subject:  iam.Subject{ID: "alice", TenantID: "t-stored", Attributes: map[string]interface{}{"dept": "eng"}},
		Action:   "read",
		Resource: "docs/plan",
	}
	_, err = engine.SimulatePolicy(ctx, &iam.PolicySimulationRequest{Policy: flagged, CheckRequest: check})
	var loadErr *iam.StoredPolicyDecodeError
	if !errors.As(err, &loadErr) || loadErr.Name != "stored-bad" {
		t.Fatalf("SimulatePolicy() error = %v; want the record's StoredPolicyDecodeError", err)
	}
	result, err := newQRDBPolicySimulator().Simulate(ctx, flagged, check)
	if err != nil {
		t.Fatalf("Simulate() error = %v", err)
	}
	if result.Allowed || !strings.Contains(result.Reason, "metadata.owner") {
		t.Fatalf("SimulatePolicy() = allowed %v, reason %q; want deny naming metadata.owner", result.Allowed, result.Reason)
	}

	log := &storedPolicyWarnLog{}
	converted, err := policiesToABAC(listed, log)
	if err != nil {
		t.Fatalf("policiesToABAC() error = %v", err)
	}
	if len(converted) != 2 {
		t.Fatalf("policiesToABAC() converted %d policies; want stored-eng and the deny standing in for stored-bad", len(converted))
	}
	if len(log.lines) != 1 || !strings.Contains(log.lines[0], `name="stored-bad"`) || !strings.Contains(log.lines[0], `unknown_keys="metadata.owner"`) {
		t.Fatalf("WARN lines = %q; want one naming stored-bad and metadata.owner", log.lines)
	}
}

// A stored record the JSON decoder cannot read fails GetPolicy with an error
// rather than reaching the evaluator.
func TestStoredPolicy_UndecodableRecordGetsFlaggedEntry(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	if err := kv.Put(ctx, iam.KeyPrefixTenant+"t-stored"+iam.KeyPrefixPolicy+"corrupt", []byte(`{"spec": {"rules": 1}}`)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:   iam.NewBackendStorage(kv),
		Simulator: newQRDBPolicySimulator(),
	})

	policy, err := engine.GetPolicy(ctx, "t-stored", "corrupt")
	if err != nil {
		t.Fatalf("GetPolicy() error = %v, want the flagged entry", err)
	}
	if policy.LoadError == nil || len(policy.Spec.Rules) != 0 {
		t.Fatalf("GetPolicy() = %+v, want a rule-less entry carrying its load error", policy)
	}
}

// allowAllAuthorizer stands in for an RBAC fallback that admits every
// request, so a test sees whether the ABAC layer itself denies.
type allowAllAuthorizer struct{}

func (allowAllAuthorizer) Authorize(_ context.Context, _ *qrdbsdk.Identity, _, _ string) error {
	return nil
}

// TestStoredPolicy_FlaggedRecordDeniesItsTenant pins that a stored record
// that does not decode is never skipped: its rules are unknown and may have
// been denies, so every request in its tenant is denied, even next to an
// allow policy and over an RBAC fallback that allows. Other tenants are
// unaffected, and repairing or deleting the record restores evaluation.
func TestStoredPolicy_FlaggedRecordDeniesItsTenant(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	allowA := strings.Replace(storedPolicyJSON, `"tenant": "t-stored"`, `"tenant": "t-a"`, 1)
	allowB := strings.Replace(storedPolicyJSON, `"tenant": "t-stored"`, `"tenant": "t-b"`, 1)
	denyBody := strings.Replace(strings.Replace(allowA, `"effect": "allow"`, `"effect": "deny"`, 1),
		`"name": "stored-eng"`, `"name": "deny-docs"`, 1)
	flaggedDeny := strings.Replace(denyBody, `"name": "deny-docs"`, `"name": "deny-docs", "owner": "ops"`, 1)
	records := map[string]string{
		iam.KeyPrefixTenant + "t-a" + iam.KeyPrefixPolicy + "stored-eng": allowA,
		iam.KeyPrefixTenant + "t-a" + iam.KeyPrefixPolicy + "deny-docs":  flaggedDeny,
		iam.KeyPrefixTenant + "t-b" + iam.KeyPrefixPolicy + "stored-eng": allowB,
	}
	for key, body := range records {
		if err := kv.Put(ctx, key, []byte(body)); err != nil {
			t.Fatalf("Put(%s) error = %v", key, err)
		}
	}
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewBackendStorage(kv)})

	authorize := func(tenant string) error {
		t.Helper()
		var listed []*iam.Policy
		for _, id := range []string{"t-a", "t-b"} {
			policies, err := engine.ListPolicies(ctx, id, iam.ListOptions{})
			if err != nil {
				t.Fatalf("ListPolicies(%s) error = %v", id, err)
			}
			listed = append(listed, policies...)
		}
		converted, err := policiesToABAC(listed, nil)
		if err != nil {
			t.Fatalf("policiesToABAC() error = %v", err)
		}
		decorator, err := qrdbsdk.DecorateAuthorizer(allowAllAuthorizer{}, qrdbsdk.ABACWithPolicies(converted))
		if err != nil {
			t.Fatalf("DecorateAuthorizer() error = %v", err)
		}
		subject := &iam.Subject{ID: "alice", TenantID: tenant, Attributes: map[string]interface{}{"dept": "eng"}}
		return decorator.Authorize(qrdbsdk.ContextWithTenant(ctx, tenant), subjectToABACIdentity(subject), "docs/plan", "read")
	}

	var denied *qrdbsdk.ABACPolicyDeniedError
	if err := authorize("t-a"); !errors.As(err, &denied) || denied.Policy != "deny-docs" {
		t.Fatalf("Authorize(t-a) with flagged deny-docs = %v; want a deny by deny-docs", err)
	}
	if err := authorize("t-b"); err != nil {
		t.Fatalf("Authorize(t-b) = %v; want allow, the flagged record is in t-a", err)
	}

	repaired := &iam.Policy{}
	if err := json.Unmarshal([]byte(strings.Replace(allowA, `"name": "stored-eng"`, `"name": "deny-docs"`, 1)), repaired); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if err := engine.UpdatePolicy(ctx, "t-a", repaired); err != nil {
		t.Fatalf("UpdatePolicy() repair error = %v", err)
	}
	if err := authorize("t-a"); err != nil {
		t.Fatalf("Authorize(t-a) after repair = %v; want allow", err)
	}

	if err := kv.Put(ctx, iam.KeyPrefixTenant+"t-a"+iam.KeyPrefixPolicy+"deny-docs", []byte(flaggedDeny)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := engine.DeletePolicy(ctx, "t-a", "deny-docs"); err != nil {
		t.Fatalf("DeletePolicy() error = %v", err)
	}
	if err := authorize("t-a"); err != nil {
		t.Fatalf("Authorize(t-a) after delete = %v; want allow", err)
	}
}

// TestFlaggedRecordDenyPolicy_UnknownTenantDeniesEverywhere pins that a
// flagged record whose tenant is unknown is scoped to the system tenant,
// whose policies apply to every tenant.
func TestFlaggedRecordDenyPolicy_UnknownTenantDeniesEverywhere(t *testing.T) {
	flagged := &iam.Policy{LoadError: &iam.StoredPolicyDecodeError{Name: "lost", Reason: "bad"}}
	converted, err := policiesToABAC([]*iam.Policy{flagged}, nil)
	if err != nil {
		t.Fatalf("policiesToABAC() error = %v", err)
	}
	decorator, err := qrdbsdk.DecorateAuthorizer(allowAllAuthorizer{}, qrdbsdk.ABACWithPolicies(converted))
	if err != nil {
		t.Fatalf("DecorateAuthorizer() error = %v", err)
	}
	ctx := qrdbsdk.ContextWithTenant(context.Background(), "t-any")
	identity := subjectToABACIdentity(&iam.Subject{ID: "alice", TenantID: "t-any"})
	var denied *qrdbsdk.ABACPolicyDeniedError
	if err := decorator.Authorize(ctx, identity, "docs/plan", "read"); !errors.As(err, &denied) || denied.Policy != "lost" {
		t.Fatalf("Authorize() = %v; want a deny by lost", err)
	}
}
