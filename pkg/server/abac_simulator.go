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
	"context"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// denyAllAuthorizer is the "inner" authorizer a scratch ABAC decorator
// wraps for policy simulation: with no RBAC grant possible in a one-off,
// throwaway decorator, every request that no rule of the single draft
// policy explicitly resolves must default-deny, mirroring the deleted
// local evaluator's historical "no matching allow policy" semantics.
type denyAllAuthorizer struct{}

// SimulationDefaultDenyError is returned by denyAllAuthorizer for any
// request the draft policy under simulation does not explicitly resolve.
type SimulationDefaultDenyError struct{}

// Error implements the error interface.
func (e *SimulationDefaultDenyError) Error() string {
	return "iam: no matching allow policy"
}

// Authorize always denies.
func (denyAllAuthorizer) Authorize(_ context.Context, _ *qrdbsdk.Identity, _, _ string) error {
	return &SimulationDefaultDenyError{}
}

// Compile-time interface check.
var _ qrdbsdk.Authorizer = denyAllAuthorizer{}

// qrdbPolicySimulator implements iam.PolicySimulator (P6.1 end-state) by
// building a scratch qrdb ABAC decorator scoped to exactly the one draft
// policy under test, wrapping a deny-by-default inner authorizer. This
// evaluates in complete isolation from the node's live enforcement state:
// no other stored policy and no RBAC grant participate, matching
// SimulatePolicy's historical single-policy dry-run contract.
type qrdbPolicySimulator struct{}

// newQRDBPolicySimulator builds the PolicySimulator iam.PolicyEngine.
// SimulatePolicy delegates to.
func newQRDBPolicySimulator() *qrdbPolicySimulator {
	return &qrdbPolicySimulator{}
}

// Compile-time interface check.
var _ iam.PolicySimulator = (*qrdbPolicySimulator)(nil)

// subjectToABACIdentity projects an iam.Subject onto qrdb's auth.Identity
// contract (qrdbsdk.Identity), the shape abac.Decorator.Authorize expects --
// distinct from qrdbtransport.Identity (subjectToQRDBIdentity,
// pep_rbac_authorizer.go), which is the embedded client's own wire type for
// the SAME conceptual identity.
func subjectToABACIdentity(subject *iam.Subject) *qrdbsdk.Identity {
	if subject == nil {
		return &qrdbsdk.Identity{}
	}
	tenantID := subject.TenantID
	if tenantID == "" {
		tenantID = subject.Tenant
	}
	identity := &qrdbsdk.Identity{
		Subject:  subject.ID,
		TenantID: tenantID,
		Roles:    subject.Roles,
	}
	if len(subject.Attributes) > 0 {
		identity.Metadata = make(map[string]string, len(subject.Attributes))
		for k, v := range subject.Attributes {
			if s, ok := v.(string); ok {
				identity.Metadata[k] = s
			}
		}
	}
	return identity
}

// Simulate evaluates req against policy in isolation. See the SCOPE NOTE on
// iam.PolicyEngine.SimulatePolicy: qrdb's Authorizer interface exposes no
// step-by-step trace and no matched-rule name on the ALLOW path (only the
// DENY path's PolicyDeniedError carries Policy/Rule names), so
// MatchedRule/EvaluationSteps are always empty on allow.
func (s *qrdbPolicySimulator) Simulate(ctx context.Context, policy *iam.Policy, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if policy != nil && policy.LoadError != nil {
		// A stored record that did not decode strictly is never applied.
		return &iam.CheckResponse{
			Allowed:       false,
			MatchedPolicy: policy.Metadata.Name,
			Reason:        policy.LoadError.Error(),
		}, nil
	}
	abacPolicy, err := policyToABAC(policy)
	if err != nil {
		return nil, err
	}
	decorator, err := qrdbsdk.DecorateAuthorizer(denyAllAuthorizer{},
		qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{abacPolicy}))
	if err != nil {
		return nil, err
	}

	identity := subjectToABACIdentity(&req.Subject)
	simCtx := qrdbsdk.ContextWithTenant(ctx, policy.Metadata.Tenant)
	authErr := decorator.Authorize(simCtx, identity, req.Resource, req.Action)

	if authErr == nil {
		return &iam.CheckResponse{
			Allowed:       true,
			MatchedPolicy: policy.Metadata.Name,
			Reason:        "allowed by policy '" + policy.Metadata.Name + "'",
		}, nil
	}

	var denied *qrdbsdk.ABACPolicyDeniedError
	if errors.As(authErr, &denied) {
		return &iam.CheckResponse{
			Allowed:       false,
			MatchedPolicy: denied.Policy,
			MatchedRule:   denied.Rule,
			Reason:        "denied by policy '" + denied.Policy + "', rule '" + denied.Rule + "'",
		}, nil
	}

	var defaultDeny *SimulationDefaultDenyError
	if errors.As(authErr, &defaultDeny) {
		return &iam.CheckResponse{Allowed: false, Reason: "default deny"}, nil
	}

	return nil, authErr
}
