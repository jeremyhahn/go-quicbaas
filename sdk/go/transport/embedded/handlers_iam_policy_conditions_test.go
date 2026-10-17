// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package embedded

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// policyCreateEnvelope mirrors the SDK client's create envelope, which the
// handler reads by field name.
type policyCreateEnvelope struct {
	TenantID string
	Request  *iamsdk.CreatePolicyRequest
}

const policyConditionsTenant = "t1"

func specYAMLWithWhen(when string) []byte {
	return []byte(`rules:
  - name: r1
    effect: allow
    actions: ["read"]
    resources: ["doc/*"]
    when:
` + when)
}

func createPolicyViaEmbedded(t *testing.T, svc *mockQuicBaaSWithAuthorizedAdmin, req *iamsdk.CreatePolicyRequest) error {
	t.Helper()
	_, err := handleIAMPolicyCreate(context.Background(), svc,
		&policyCreateEnvelope{TenantID: policyConditionsTenant, Request: req})
	return classifyServiceError("IAM.Policy", "Create", err)
}

// newPolicyStoreTestService returns the authorized-admin double with a
// policy engine backed by in-memory storage, so a test can read back what
// the handler stored.
func newPolicyStoreTestService(t *testing.T) *mockQuicBaaSWithAuthorizedAdmin {
	t.Helper()
	svc := newAuthorizedAdminTestService(t)
	svc.policy = iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewMemoryStorage(),
		RBACAuthorizer: allowAllAuthorizer{},
	})
	return svc
}

func assertPolicyNotStored(t *testing.T, svc *mockQuicBaaSWithAuthorizedAdmin, name string) {
	t.Helper()
	_, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, name)
	assert.Error(t, err, "refused policy was stored")
}

func TestHandleIAMPolicyCreate_SpecYAMLNestedConditionRefused(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	err := createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
		Name:     "p1",
		SpecYAML: specYAMLWithWhen("      - or:\n          - expression: \"x == 1\"\n"),
	})

	var nested *iam.NestedConditionError
	require.True(t, stderrors.As(err, &nested), "expected NestedConditionError, got %v", err)
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
	assertPolicyNotStored(t, svc, "p1")
}

func TestHandleIAMPolicyCreate_SpecYAMLFlatConditionStored(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	err := createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
		Name:     "p1",
		SpecYAML: specYAMLWithWhen("      - expression: \"x == 1\"\n"),
	})
	require.NoError(t, err)

	stored, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, "p1")
	require.NoError(t, err)
	require.Len(t, stored.Spec.Rules, 1)
	require.Len(t, stored.Spec.Rules[0].When, 1)
	assert.Equal(t, "x == 1", stored.Spec.Rules[0].When[0].Expression)
	assert.Equal(t, policyConditionsTenant, stored.Metadata.Tenant)
}

// TestHandleIAMPolicyCreate_SpecYAMLDocumentName pins that a spec_yaml
// document naming another policy is refused and nothing is stored, while a
// matching document and a bare spec are stored under the request's name.
func TestHandleIAMPolicyCreate_SpecYAMLDocumentName(t *testing.T) {
	fullDocument := func(name string) []byte {
		return []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: ` + name + `
spec:
  rules:
    - name: r1
      effect: allow
      actions: ["read"]
      resources: ["doc/*"]
`)
	}
	tests := []struct {
		name     string
		specYAML []byte
		wantErr  bool
	}{
		{"mismatch refused", fullDocument("other"), true},
		{"match accepted", fullDocument("p1"), false},
		{"bare spec takes the request name", specYAMLWithWhen("      - expression: \"x == 1\"\n"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newPolicyStoreTestService(t)
			err := createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
				Name:     "p1",
				SpecYAML: tt.specYAML,
			})
			if tt.wantErr {
				require.Error(t, err)
				want := (&iam.PolicyNameMismatchError{Requested: "p1", Document: "other"}).Error()
				assert.Contains(t, err.Error(), want)
				assertPolicyNotStored(t, svc, "p1")
				assertPolicyNotStored(t, svc, "other")
				return
			}
			require.NoError(t, err)
			stored, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, "p1")
			require.NoError(t, err)
			assert.Equal(t, "p1", stored.Metadata.Name)
		})
	}
}

func TestHandleIAMPolicyCreate_BadSpecYAMLRefused(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	err := createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
		Name:     "p1",
		SpecYAML: []byte("rules: [unclosed"),
	})

	var yamlErr *iam.PolicyYAMLError
	require.True(t, stderrors.As(err, &yamlErr), "expected PolicyYAMLError, got %v", err)
	assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
	assertPolicyNotStored(t, svc, "p1")
}

func TestHandleIAMPolicyCreate_UntranslatableFieldsRefused(t *testing.T) {
	rule := iamsdk.Rule{
		Name:      "r1",
		Effect:    iamsdk.EffectAllow,
		Actions:   []iamsdk.ActionMatcher{{Pattern: "read"}},
		Resources: []iamsdk.ResourceMatcher{{Pattern: "doc/*"}},
	}
	condition := iamsdk.Condition{Key: "source_ip", Value: "10.0.0.0/8"}
	ruleWithCondition := rule
	ruleWithCondition.Conditions = []iamsdk.Condition{condition}
	ruleWithSubject := rule
	ruleWithSubject.Subjects = []iamsdk.SubjectMatcher{{Type: iamsdk.SubjectTypeUser, ID: "alice"}}
	target := iamsdk.Target{ResourceTypes: []string{"doc"}}

	tests := []struct {
		name      string
		req       *iamsdk.CreatePolicyRequest
		wantField string
	}{
		{
			name:      "policy level",
			req:       &iamsdk.CreatePolicyRequest{Name: "p1", Rules: []iamsdk.Rule{rule}, Conditions: []iamsdk.Condition{condition}},
			wantField: "conditions",
		},
		{
			name:      "rule level",
			req:       &iamsdk.CreatePolicyRequest{Name: "p1", Rules: []iamsdk.Rule{rule, ruleWithCondition}},
			wantField: "rules[1].conditions",
		},
		{
			name:      "rule subjects",
			req:       &iamsdk.CreatePolicyRequest{Name: "p1", Rules: []iamsdk.Rule{rule, ruleWithSubject}},
			wantField: "rules[1].subjects",
		},
		{
			name:      "targets",
			req:       &iamsdk.CreatePolicyRequest{Name: "p1", Rules: []iamsdk.Rule{rule}, Targets: []iamsdk.Target{target}},
			wantField: "targets",
		},
		{
			name: "targets alongside spec_yaml",
			req: &iamsdk.CreatePolicyRequest{
				Name:     "p1",
				SpecYAML: specYAMLWithWhen("      - expression: \"x == 1\"\n"),
				Targets:  []iamsdk.Target{target},
			},
			wantField: "targets",
		},
		{
			name:      "policy-level effect allow",
			req:       &iamsdk.CreatePolicyRequest{Name: "p1", Rules: []iamsdk.Rule{rule}, Effect: iamsdk.EffectAllow},
			wantField: "effect",
		},
		{
			name: "policy-level effect allow alongside spec_yaml",
			req: &iamsdk.CreatePolicyRequest{
				Name:     "p1",
				SpecYAML: specYAMLWithWhen("      - expression: \"x == 1\"\n"),
				Effect:   iamsdk.EffectAllow,
			},
			wantField: "effect",
		},
		{
			name: "alongside spec_yaml",
			req: &iamsdk.CreatePolicyRequest{
				Name:       "p1",
				SpecYAML:   specYAMLWithWhen("      - expression: \"x == 1\"\n"),
				Conditions: []iamsdk.Condition{condition},
			},
			wantField: "conditions",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newPolicyStoreTestService(t)
			err := createPolicyViaEmbedded(t, svc, tc.req)

			var unsupported *UnsupportedPolicyFieldError
			require.True(t, stderrors.As(err, &unsupported), "expected UnsupportedPolicyFieldError, got %v", err)
			assert.Equal(t, tc.wantField, unsupported.Field)
			assert.Equal(t, sdkerrors.CodeInvalidInput, sdkerrors.Code(err))
			assertPolicyNotStored(t, svc, "p1")
		})
	}
}

func TestHandleIAMPolicyCreate_RulesWithoutConditionsStored(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	err := createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
		Name: "p1",
		Rules: []iamsdk.Rule{{
			Name:      "r1",
			Effect:    iamsdk.EffectDeny,
			Actions:   []iamsdk.ActionMatcher{{Pattern: "read"}},
			Resources: []iamsdk.ResourceMatcher{{Pattern: "doc/*"}},
		}},
	})
	require.NoError(t, err)

	stored, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, "p1")
	require.NoError(t, err)
	require.Len(t, stored.Spec.Rules, 1)
	assert.Equal(t, iam.EffectDeny, stored.Spec.Rules[0].Effect)
	assert.Equal(t, []string{"read"}, stored.Spec.Rules[0].Actions)
}

// TestHandleIAMPolicyCreate_PolicyLevelDefaultsStored checks that the
// policy-level values the engine stores anyway - effect deny and enabled -
// are accepted, as policyfields.Refuse accepts them on gRPC and MCP.
func TestHandleIAMPolicyCreate_PolicyLevelDefaultsStored(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	err := createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
		Name:    "p1",
		Enabled: true,
		Effect:  iamsdk.EffectDeny,
		Rules: []iamsdk.Rule{{
			Name:      "r1",
			Effect:    iamsdk.EffectAllow,
			Actions:   []iamsdk.ActionMatcher{{Pattern: "read"}},
			Resources: []iamsdk.ResourceMatcher{{Pattern: "doc/*"}},
		}},
	})
	require.NoError(t, err)

	stored, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, "p1")
	require.NoError(t, err)
	require.Len(t, stored.Spec.Rules, 1)
	assert.Equal(t, iam.EffectAllow, stored.Spec.Rules[0].Effect)
}

func TestUnsupportedPolicyFieldError_Message(t *testing.T) {
	effect := &UnsupportedPolicyFieldError{Service: "quicbaas.IAM.Policy", Method: "Create", Field: "effect"}
	assert.Contains(t, effect.Error(), "deny by default")

	targets := &UnsupportedPolicyFieldError{Service: "quicbaas.IAM.Policy", Method: "Create", Field: "targets"}
	assert.Contains(t, targets.Error(), "spec_yaml")
}
