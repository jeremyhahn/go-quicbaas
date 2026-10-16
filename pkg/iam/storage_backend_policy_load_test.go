package iam

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storedPolicyJSON is a policy record as an older build wrote it: the JSON
// encoding of a Policy, put straight into the KV store at policyKey. It was
// never through ParsePolicyYAML's strict decode.
const storedPolicyJSON = `{
  "apiVersion": "iam.quicbaas.io/v1",
  "kind": "Policy",
  "metadata": {
    "name": "stored-eng",
    "tenant": "t-stored",
    "description": "written before the strict YAML parse",
    "labels": {"team": "eng"},
    "priority": 7,
    "created": "2026-01-02T03:04:05Z",
    "updated": "2026-01-03T03:04:05Z"
  },
  "spec": {
    "subjects": {"authenticated": true},
    "variables": {"dept": "subject.dept"},
    "rules": [
      {
        "name": "eng-read",
        "effect": "allow",
        "priority": 10,
        "actions": ["read"],
        "resources": ["docs/*"],
        "when": [{"expression": "subject.dept == 'eng'"}],
        "unless": [{"expression": "subject.dept == 'ops'"}]
      },
      {
        "name": "deny-delete",
        "effect": "deny",
        "actions": ["delete"],
        "resources": ["docs/*"]
      }
    ]
  }
}`

// storedPolicyUnknownKeysJSON carries keys ParsePolicyYAML refuses: a
// rule-level "subjects" and a condition-level "none".
const storedPolicyUnknownKeysJSON = `{
  "apiVersion": "iam.quicbaas.io/v1",
  "kind": "Policy",
  "metadata": {"name": "stored-unknown", "tenant": "t-stored"},
  "spec": {
    "subjects": {"authenticated": true},
    "rules": [
      {
        "name": "eng-read",
        "effect": "allow",
        "actions": ["read"],
        "resources": ["docs/*"],
        "subjects": {"users": ["alice"]},
        "when": [{"expression": "subject.dept == 'eng'", "none": [{"expression": "subject.dept == 'ops'"}]}]
      }
    ]
  }
}`

func wantStoredPolicy() *Policy {
	return &Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        "stored-eng",
			Tenant:      "t-stored",
			Description: "written before the strict YAML parse",
			Labels:      map[string]string{"team": "eng"},
			Priority:    7,
			Created:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			Updated:     time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC),
		},
		Spec: PolicySpec{
			Subjects:  SubjectSelector{Authenticated: true},
			Variables: map[string]string{"dept": "subject.dept"},
			Rules: []Rule{
				{
					Name:      "eng-read",
					Effect:    EffectAllow,
					Priority:  10,
					Actions:   []string{"read"},
					Resources: []string{"docs/*"},
					When:      []Condition{{Expression: "subject.dept == 'eng'"}},
					Unless:    []Condition{{Expression: "subject.dept == 'ops'"}},
				},
				{
					Name:      "deny-delete",
					Effect:    EffectDeny,
					Actions:   []string{"delete"},
					Resources: []string{"docs/*"},
				},
			},
		},
	}
}

func findPolicy(policies []*Policy, name string) *Policy {
	for _, p := range policies {
		if p.Metadata.Name == name {
			return p
		}
	}
	return nil
}

func TestBackendStorage_StoredPolicyJSONLoadsWhole(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-eng"), []byte(storedPolicyJSON)))

	got, err := storage.GetPolicy(ctx, "t-stored", "stored-eng")
	require.NoError(t, err)
	assert.Equal(t, wantStoredPolicy(), got)
	require.NoError(t, got.Validate())

	listed, err := storage.ListPolicies(ctx, "t-stored", ListOptions{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, wantStoredPolicy(), listed[0])
}

// TestBackendStorage_StoredPolicyUnknownKeysNotApplied pins the fail-closed
// load: a record with keys the strict decoder refuses is never returned as a
// policy. GetPolicy returns the typed error naming every key, ListPolicies
// lists it as a flagged entry with no rules next to the good record, and
// both log it at WARN.
func TestBackendStorage_StoredPolicyUnknownKeysNotApplied(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	logger := &capturingLogger{}
	storage := NewBackendStorage(kv, WithStorageLogger(logger))
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-eng"), []byte(storedPolicyJSON)))
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-unknown"), []byte(storedPolicyUnknownKeysJSON)))
	wantKeys := []string{"spec.rules[0].subjects", "spec.rules[0].when[0].none"}

	got, err := storage.GetPolicy(ctx, "t-stored", "stored-unknown")
	assert.Nil(t, got)
	require.ErrorIs(t, err, ErrPolicyRecordInvalid)
	var loadErr *StoredPolicyDecodeError
	require.ErrorAs(t, err, &loadErr)
	assert.Equal(t, "t-stored", loadErr.TenantID)
	assert.Equal(t, "stored-unknown", loadErr.Name)
	assert.Equal(t, wantKeys, loadErr.UnknownKeys)
	require.Len(t, logger.warnings, 1)
	assert.Contains(t, logger.warnings[0], `name="stored-unknown"`)
	assert.Contains(t, logger.warnings[0], `unknown_keys="spec.rules[0].subjects,spec.rules[0].when[0].none"`)

	listed, err := storage.ListPolicies(ctx, "t-stored", ListOptions{})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, wantStoredPolicy(), findPolicy(listed, "stored-eng"))
	flagged := findPolicy(listed, "stored-unknown")
	require.NotNil(t, flagged)
	require.NotNil(t, flagged.LoadError)
	assert.Equal(t, wantKeys, flagged.LoadError.UnknownKeys)
	assert.Empty(t, flagged.Spec.Rules, "a flagged entry must carry no rules")
	assert.Equal(t, "t-stored", flagged.Metadata.Tenant)
	assert.Len(t, logger.warnings, 2)
}

// TestBackendStorage_ListPoliciesFlagsUndecodableRecord pins that a record
// that is not a policy document at all is listed as flagged, not dropped.
func TestBackendStorage_ListPoliciesFlagsUndecodableRecord(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-eng"), []byte(storedPolicyJSON)))
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "corrupt"), []byte(`{"spec": {"rules": "not-a-list"}}`)))

	listed, err := storage.ListPolicies(ctx, "t-stored", ListOptions{})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Nil(t, findPolicy(listed, "stored-eng").LoadError)
	flagged := findPolicy(listed, "corrupt")
	require.NotNil(t, flagged)
	require.NotNil(t, flagged.LoadError)
	assert.Empty(t, flagged.LoadError.UnknownKeys)
	assert.NotEmpty(t, flagged.LoadError.Reason)

	_, err = storage.GetPolicy(ctx, "t-stored", "corrupt")
	var loadErr *StoredPolicyDecodeError
	require.ErrorAs(t, err, &loadErr)
	assert.Equal(t, "corrupt", loadErr.Name)
}

// TestDecodeStoredPolicy covers the strict decoder's edges.
func TestDecodeStoredPolicy(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantKeys []string
		wantErr  bool
	}{
		{name: "case-insensitive key is a field", data: `{"Kind": "Policy", "spec": {"rules": [{"Effect": "allow"}]}}`},
		{name: "trailing data", data: `{"kind": "Policy"} {}`, wantErr: true},
		{name: "not JSON", data: `not-json`, wantErr: true},
		{name: "json-hidden field is unknown", data: `{"kind": "Policy", "LoadError": {}}`, wantKeys: []string{"LoadError"}, wantErr: true},
		{name: "keys under a free-form map are not checked", data: `{"spec": {"subjects": {"match": {"x": {"y": 1}}}}}`},
		{name: "top-level unknown key", data: `{"kind": "Policy", "extra": 1}`, wantKeys: []string{"extra"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, loadErr := decodeStoredPolicy("t", "p", []byte(tt.data))
			if !tt.wantErr {
				require.Nil(t, loadErr)
				require.NotNil(t, policy)
				return
			}
			require.NotNil(t, loadErr)
			assert.Nil(t, policy)
			assert.Equal(t, tt.wantKeys, loadErr.UnknownKeys)
			assert.Contains(t, loadErr.Error(), "stored policy 'p' in tenant 't' is not applied")
		})
	}
}

// TestPolicyEngine_RepairsFlaggedRecord pins that a refused record can still
// be replaced or deleted: that is how an operator repairs it.
func TestPolicyEngine_RepairsFlaggedRecord(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: NewBackendStorage(kv)})
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-eng"), []byte(storedPolicyUnknownKeysJSON)))

	replacement := wantStoredPolicy()
	require.NoError(t, engine.UpdatePolicy(ctx, "t-stored", replacement))
	got, err := engine.GetPolicy(ctx, "t-stored", "stored-eng")
	require.NoError(t, err)
	assert.Equal(t, replacement.Spec.Rules, got.Spec.Rules)

	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-eng"), []byte(`{"bogus": true}`)))
	require.NoError(t, engine.DeletePolicy(ctx, "t-stored", "stored-eng"))
	_, err = engine.GetPolicy(ctx, "t-stored", "stored-eng")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrPolicyRecordInvalid)
}

// failingABACSync is an ABACSync whose reload always fails, so the engine
// rolls back the write it just made.
type failingABACSync struct{}

func (failingABACSync) Sync(context.Context) error {
	return &IAMMessageError{Message: "reload refused"}
}

// TestPolicyEngine_RollbackRestoresFlaggedRecordBytes pins that a repair or
// delete of a flagged record whose ABAC sync fails restores the record's
// original stored bytes, so it stays byte-identical and still flagged.
func TestPolicyEngine_RollbackRestoresFlaggedRecordBytes(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: NewBackendStorage(kv), ABACSync: failingABACSync{}})
	key := policyKey("t-stored", "stored-eng")
	require.NoError(t, kv.Put(ctx, key, []byte(storedPolicyUnknownKeysJSON)))

	assertOriginal := func(op string) {
		t.Helper()
		got, err := kv.Get(ctx, key)
		require.NoError(t, err, op)
		assert.Equal(t, storedPolicyUnknownKeysJSON, string(got), "%s rollback must restore the original bytes", op)
		_, err = engine.GetPolicy(ctx, "t-stored", "stored-eng")
		assert.ErrorIs(t, err, ErrPolicyRecordInvalid, "%s rollback must leave the record flagged", op)
	}

	require.Error(t, engine.UpdatePolicy(ctx, "t-stored", wantStoredPolicy()))
	assertOriginal("update")

	require.Error(t, engine.DeletePolicy(ctx, "t-stored", "stored-eng"))
	assertOriginal("delete")
}

// newStoredPolicyBatchManager returns a BatchManager over an in-memory store
// holding the good record and, when withFlagged is set, a record with
// unknown keys.
func newStoredPolicyBatchManager(t *testing.T, withFlagged bool) *BatchManager {
	t.Helper()
	ctx := context.Background()
	kv := newMockKVStore()
	require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-eng"), []byte(storedPolicyJSON)))
	if withFlagged {
		require.NoError(t, kv.Put(ctx, policyKey("t-stored", "stored-unknown"), []byte(storedPolicyUnknownKeysJSON)))
	}
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: NewBackendStorage(kv)})
	return NewBatchManager(BatchManagerOptions{PolicyEngine: engine})
}

func TestBatchManager_ExportPoliciesRefusesFlaggedEntry(t *testing.T) {
	ctx := context.Background()

	exported, err := newStoredPolicyBatchManager(t, false).ExportPolicies(ctx, "t-stored", ExportFilter{})
	require.NoError(t, err)
	require.Len(t, exported, 1)
	assert.Equal(t, "stored-eng", exported[0].Metadata.Name)

	exported, err = newStoredPolicyBatchManager(t, true).ExportPolicies(ctx, "t-stored", ExportFilter{})
	assert.Nil(t, exported, "an export with a flagged entry must return no policies")
	require.ErrorIs(t, err, ErrPolicyRecordInvalid)
	var loadErr *StoredPolicyDecodeError
	require.ErrorAs(t, err, &loadErr)
	assert.Equal(t, "stored-unknown", loadErr.Name)
}

func TestBatchManager_ExportPoliciesErrorNamesPolicy(t *testing.T) {
	_, err := newStoredPolicyBatchManager(t, true).ExportPolicies(context.Background(), "t-stored", ExportFilter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'stored-unknown'")
	assert.Contains(t, err.Error(), "'t-stored'")
	assert.NotContains(t, err.Error(), "'stored-eng'")
}
