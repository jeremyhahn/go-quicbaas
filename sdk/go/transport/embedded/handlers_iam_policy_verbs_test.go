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

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// The envelopes mirror the SDK policy client's internal request structs.
type policyUpdateEnvelope struct {
	TenantID   string
	PolicyName string
	Request    *iamsdk.UpdatePolicyRequest
}

type policyValidateEnvelope struct {
	TenantID string
	Policy   *iamsdk.Policy
	Name     string
}

type policyTestEnvelope struct {
	TenantID string
	Request  *iamsdk.TestPolicyRequest
}

type policyGetEnvelope struct {
	TenantID   string
	PolicyName string
}

const allowReadSpec = `rules:
  - name: r1
    effect: allow
    actions: ["read"]
    resources: ["doc/*"]
`

func newPolicyVerbsService(t *testing.T) *mockQuicBaaSWithAuthorizedAdmin {
	t.Helper()
	svc := newPolicyStoreTestService(t)
	require.NoError(t, createPolicyViaEmbedded(t, svc, &iamsdk.CreatePolicyRequest{
		Name:     "p1",
		SpecYAML: []byte(allowReadSpec),
	}))
	return svc
}

func strPtr(s string) *string { return &s }

func TestHandleIAMPolicyUpdate_VisibleToGet(t *testing.T) {
	svc := newPolicyVerbsService(t)
	priority := 7
	out, err := handleIAMPolicyUpdate(context.Background(), svc, &policyUpdateEnvelope{
		TenantID:   policyConditionsTenant,
		PolicyName: "p1",
		Request: &iamsdk.UpdatePolicyRequest{
			Description: strPtr("updated"),
			Priority:    &priority,
			Labels:      map[string]string{"team": "a"},
		},
	})
	require.NoError(t, err)
	updated, ok := out.(*iamsdk.Policy)
	require.True(t, ok)
	assert.Equal(t, "updated", updated.Description)

	got, err := handleIAMPolicyGet(context.Background(), svc,
		&policyGetEnvelope{TenantID: policyConditionsTenant, PolicyName: "p1"})
	require.NoError(t, err)
	p := got.(*iamsdk.Policy)
	assert.Equal(t, "updated", p.Description)
	assert.Equal(t, 7, p.Priority)
	assert.Equal(t, map[string]string{"team": "a"}, p.Labels)

	// Rules were not sent, so the stored ones survive.
	stored, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, "p1")
	require.NoError(t, err)
	require.Len(t, stored.Spec.Rules, 1)
	assert.Equal(t, "r1", stored.Spec.Rules[0].Name)
}

func TestHandleIAMPolicyUpdate_SpecYAMLReplacesRules(t *testing.T) {
	svc := newPolicyVerbsService(t)
	spec := []byte(`rules:
  - name: r2
    effect: allow
    actions: ["write"]
    resources: ["doc/*"]
`)
	_, err := handleIAMPolicyUpdate(context.Background(), svc, &policyUpdateEnvelope{
		TenantID:   policyConditionsTenant,
		PolicyName: "p1",
		Request:    &iamsdk.UpdatePolicyRequest{SpecYAML: spec},
	})
	require.NoError(t, err)

	stored, err := svc.policy.GetPolicy(context.Background(), policyConditionsTenant, "p1")
	require.NoError(t, err)
	require.Len(t, stored.Spec.Rules, 1)
	assert.Equal(t, "r2", stored.Spec.Rules[0].Name)
}

func TestHandleIAMPolicyUpdate_UnknownPolicy(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	_, err := handleIAMPolicyUpdate(context.Background(), svc, &policyUpdateEnvelope{
		TenantID:   policyConditionsTenant,
		PolicyName: "missing",
		Request:    &iamsdk.UpdatePolicyRequest{Description: strPtr("x")},
	})
	require.Error(t, err)
	coded := classifyServiceError("IAM.Policy", "Update", err)
	assert.True(t, errors.IsNotFound(coded), "got %v", coded)
}

func TestHandleIAMPolicyUpdate_WrongRequestType(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	_, err := handleIAMPolicyUpdate(context.Background(), svc, &policyCreateEnvelope{
		TenantID: policyConditionsTenant,
		Request:  &iamsdk.CreatePolicyRequest{Name: "p1"},
	})
	var typeErr *InvalidRequestTypeError
	require.True(t, stderrors.As(err, &typeErr), "got %v", err)
}

func TestHandleIAMPolicyUpdate_UntranslatableFieldsRefused(t *testing.T) {
	allow := iamsdk.EffectAllow
	disabled := false
	cases := map[string]struct {
		req   *iamsdk.UpdatePolicyRequest
		field string
	}{
		"effect":     {&iamsdk.UpdatePolicyRequest{Effect: &allow}, "effect"},
		"enabled":    {&iamsdk.UpdatePolicyRequest{Enabled: &disabled}, "enabled"},
		"version":    {&iamsdk.UpdatePolicyRequest{Version: 3}, "version"},
		"conditions": {&iamsdk.UpdatePolicyRequest{Conditions: []iamsdk.Condition{{}}}, "conditions"},
		"targets":    {&iamsdk.UpdatePolicyRequest{Targets: []iamsdk.Target{{}}}, "targets"},
		"rule conditions": {&iamsdk.UpdatePolicyRequest{
			Rules: []iamsdk.Rule{{Conditions: []iamsdk.Condition{{}}}}}, "rules[0].conditions"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc := newPolicyVerbsService(t)
			_, err := handleIAMPolicyUpdate(context.Background(), svc, &policyUpdateEnvelope{
				TenantID: policyConditionsTenant, PolicyName: "p1", Request: tc.req,
			})
			var fieldErr *UnsupportedPolicyFieldError
			require.True(t, stderrors.As(err, &fieldErr), "got %v", err)
			assert.Equal(t, tc.field, fieldErr.Field)
			assert.Equal(t, "Update", fieldErr.Method)
		})
	}
}

func TestHandleIAMPolicyValidate_Valid(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	out, err := handleIAMPolicyValidate(context.Background(), svc, &policyValidateEnvelope{
		TenantID: policyConditionsTenant,
		Policy:   &iamsdk.Policy{SpecYAML: []byte(allowReadSpec)},
		Name:     "p1",
	})
	require.NoError(t, err)
	result := out.(*iamsdk.ValidationResult)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestHandleIAMPolicyValidate_InvalidPolicy(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	out, err := handleIAMPolicyValidate(context.Background(), svc, &policyValidateEnvelope{
		TenantID: policyConditionsTenant,
		Policy:   &iamsdk.Policy{SpecYAML: []byte("rules:\n  - bogus: [\n")},
		Name:     "p1",
	})
	require.NoError(t, err)
	result := out.(*iamsdk.ValidationResult)
	assert.False(t, result.Valid)
	require.Len(t, result.Errors, 1)
	assert.NotEmpty(t, result.Errors[0].Message)
}

func TestHandleIAMPolicyValidate_RuleWithoutActions(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	spec := []byte("rules:\n  - name: r1\n    effect: allow\n    resources: [\"doc/*\"]\n")
	out, err := handleIAMPolicyValidate(context.Background(), svc, &policyValidateEnvelope{
		TenantID: policyConditionsTenant,
		Policy:   &iamsdk.Policy{SpecYAML: spec},
		Name:     "p1",
	})
	require.NoError(t, err)
	result := out.(*iamsdk.ValidationResult)
	assert.False(t, result.Valid)
	require.Len(t, result.Errors, 1)
	assert.NotEmpty(t, result.Errors[0].Message)

	// Validate stores nothing.
	assertPolicyNotStored(t, svc, "p1")
}

// namedPolicyDoc is a full policy document whose metadata.name is name.
func namedPolicyDoc(name string) []byte {
	return []byte(`apiVersion: v1
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

func TestHandleIAMPolicyValidate_DocumentNameMatches(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	out, err := handleIAMPolicyValidate(context.Background(), svc, &policyValidateEnvelope{
		TenantID: policyConditionsTenant,
		Policy:   &iamsdk.Policy{SpecYAML: namedPolicyDoc("p1")},
		Name:     "p1",
	})
	require.NoError(t, err)
	result := out.(*iamsdk.ValidationResult)
	assert.True(t, result.Valid, "errors: %v", result.Errors)
}

// A document named differently from the requested name is reported invalid
// with the mismatch, as gRPC (IAMServiceAdapter.ValidatePolicy) and REST
// (?name=) report it.
func TestHandleIAMPolicyValidate_DocumentNameMismatch(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	out, err := handleIAMPolicyValidate(context.Background(), svc, &policyValidateEnvelope{
		TenantID: policyConditionsTenant,
		Policy:   &iamsdk.Policy{SpecYAML: namedPolicyDoc("other")},
		Name:     "requested",
	})
	require.NoError(t, err)
	result := out.(*iamsdk.ValidationResult)
	assert.False(t, result.Valid)
	require.Len(t, result.Errors, 1)
	want := iam.CheckPolicyName(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "other"}}, "requested").Error()
	assert.Equal(t, want, result.Errors[0].Message)
}

func TestHandleIAMPolicyValidate_MissingSpec(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	_, err := handleIAMPolicyValidate(context.Background(), svc, &policyValidateEnvelope{
		TenantID: policyConditionsTenant,
		Policy:   &iamsdk.Policy{Name: "p1"},
	})
	assert.True(t, errors.IsInvalidInput(err), "got %v", err)
}

func TestHandleIAMPolicyValidate_WrongRequestType(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	_, err := handleIAMPolicyValidate(context.Background(), svc, &policyGetEnvelope{
		TenantID: policyConditionsTenant, PolicyName: "p1",
	})
	var typeErr *InvalidRequestTypeError
	require.True(t, stderrors.As(err, &typeErr), "got %v", err)
}

func TestHandleIAMPolicyTest_Success(t *testing.T) {
	svc := newPolicyVerbsService(t)
	out, err := handleIAMPolicyTest(context.Background(), svc, &policyTestEnvelope{
		TenantID: policyConditionsTenant,
		Request: &iamsdk.TestPolicyRequest{
			PolicyName: "p1",
			TestCases: []iamsdk.TestCase{
				{
					Name: "read allowed", Subject: iamsdk.Subject{ID: "u1"},
					Action: "read", Resource: "doc/1", ExpectedResult: iamsdk.EffectAllow,
				},
				{
					// The fixture's RBAC authorizer grants every check, so
					// this decision is allow and the case fails.
					Name: "write expected deny", Subject: iamsdk.Subject{ID: "u1"},
					Action: "write", Resource: "doc/1", ExpectedResult: iamsdk.EffectDeny,
				},
			},
		},
	})
	require.NoError(t, err)
	resp := out.(*iamsdk.TestPolicyResponse)
	assert.Equal(t, 1, resp.Passed)
	assert.Equal(t, 1, resp.Failed)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, iamsdk.EffectAllow, resp.Results[0].ActualResult)
	assert.True(t, resp.Results[0].Passed)
	assert.False(t, resp.Results[1].Passed)
	assert.Equal(t, iamsdk.EffectAllow, resp.Results[1].ActualResult)
	assert.Equal(t, iamsdk.EffectDeny, resp.Results[1].ExpectedResult)
	assert.Equal(t, "read allowed", resp.Results[0].Name)
}

func TestHandleIAMPolicyTest_NoCases(t *testing.T) {
	svc := newPolicyVerbsService(t)
	_, err := handleIAMPolicyTest(context.Background(), svc, &policyTestEnvelope{
		TenantID: policyConditionsTenant,
		Request:  &iamsdk.TestPolicyRequest{PolicyName: "p1"},
	})
	assert.True(t, errors.IsInvalidInput(err), "got %v", err)
}

func TestHandleIAMPolicyTest_WrongRequestType(t *testing.T) {
	svc := newPolicyStoreTestService(t)
	_, err := handleIAMPolicyTest(context.Background(), svc, "not a request")
	var typeErr *InvalidRequestTypeError
	require.True(t, stderrors.As(err, &typeErr), "got %v", err)
}

func TestIAMDispatch_PolicyVerbsRegistered(t *testing.T) {
	for _, m := range []string{"Update", "Validate", "Test"} {
		_, ok := iamUnaryHandlers["quicbaas.IAM.Policy/"+m]
		assert.True(t, ok, m)
	}
}
