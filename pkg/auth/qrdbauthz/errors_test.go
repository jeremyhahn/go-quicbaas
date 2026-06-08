//go:build iam

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

package qrdbauthz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
)

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil resolver", &NilRBACResolverError{}, "rbac client resolver is nil"},
		{"missing tenant with subject", &MissingTenantError{Subject: "bob"}, "subject bob has no tenant"},
		{"missing tenant no subject", &MissingTenantError{}, "subject has no tenant"},
		{"pdp unavailable", &PDPUnavailableError{}, "policy decision point is unavailable"},
		{"abac with cause", &ABACEvaluationError{Cause: errors.New("boom")}, "boom"},
		{"abac no cause", &ABACEvaluationError{}, "abac evaluation failed"},
		{"denied with reason", &AuthorizationDeniedError{Resource: "keys", Action: "get", Reason: "policy x"}, "policy x"},
		{"denied no reason", &AuthorizationDeniedError{Resource: "keys", Action: "get"}, "keys:get"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.want) {
				t.Errorf("Error() = %q, want substring %q", tt.err.Error(), tt.want)
			}
		})
	}
}

func TestABACEvaluationError_Unwrap(t *testing.T) {
	cause := errors.New("root")
	err := &ABACEvaluationError{Cause: cause}
	assert.ErrorIs(t, err, cause)
}

// TestDecisionPoint_DelegatesToAdapter verifies the auth-seam bridge maps the
// auth subject onto the adapter subject and delegates to the qrdb PDP, returning
// the adapter's decision unchanged. A missing tenant surfaces the adapter's
// typed error through the bridge.
func TestDecisionPoint_DelegatesToAdapter(t *testing.T) {
	adapter, err := New(newRealRBACResolver(), nil)
	require.NoError(t, err)
	dp := NewDecisionPoint(adapter)

	// admin holds keys:get -> allow.
	err = dp.Authorize(context.Background(), auth.QRDBSubject{
		ID:       "admin",
		TenantID: qrdbtenant.SystemTenantID,
		Roles:    []string{"admin"},
	}, "keys", "get")
	require.NoError(t, err)

	// admin lacks keys:sign -> deny (qrdb typed error surfaced through bridge).
	err = dp.Authorize(context.Background(), auth.QRDBSubject{
		ID:       "admin",
		TenantID: qrdbtenant.SystemTenantID,
		Roles:    []string{"admin"},
	}, "keys", "sign")
	require.Error(t, err)
}

// TestDecisionPoint_MissingTenant verifies the bridge surfaces the adapter's
// MissingTenantError for an empty-tenant subject.
func TestDecisionPoint_MissingTenant(t *testing.T) {
	adapter, err := New(newRealRBACResolver(), nil)
	require.NoError(t, err)
	dp := NewDecisionPoint(adapter)

	err = dp.Authorize(context.Background(), auth.QRDBSubject{ID: "x", Roles: []string{"admin"}}, "keys", "get")
	require.Error(t, err)
	var missing *MissingTenantError
	assert.ErrorAs(t, err, &missing)
}

// TestAuthorize_ABACEvaluationFailure asserts an ABAC evaluator that fails (here
// a nil cache surfaced as no policies, but to force an error we use a closed
// evaluator path) does not silently allow. The evaluator returns no error for
// "no policies", so this guards the explicit-deny boundary instead: a deny with
// an empty matched policy must NOT override the allow.
func TestAuthorize_ABACEmptyEvaluatorPreservesAllow(t *testing.T) {
	// An evaluator with no policies returns Allowed=false, MatchedPolicy="" —
	// "no matching allow policy". This must preserve the RBAC allow.
	evaluator := newEmptyEvaluator(t)
	a, err := New(newRealRBACResolver(), evaluator)
	require.NoError(t, err)

	err = a.Authorize(context.Background(), adminSubject(), "keys", "get")
	require.NoError(t, err, "empty ABAC evaluator must not deny an RBAC-allowed request")
}

// newEmptyEvaluator builds a real PolicyEvaluator whose cache holds no policies,
// so Evaluate returns "no matching allow policy" with no matched policy.
func newEmptyEvaluator(t *testing.T) *iam.PolicyEvaluator {
	t.Helper()
	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{TTL: time.Hour, BloomFilterSize: 1024, BloomFilterHashes: 4})
	return iam.NewPolicyEvaluator(compiler, cache)
}
