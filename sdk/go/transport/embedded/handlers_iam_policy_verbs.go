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

// Policy Update, Validate and Test handlers (quicbaas.IAM.Policy/*).
//
// The request and response shapes are the ones the SDK policy client sends
// and the gRPC transport returns: an envelope carrying TenantID plus the SDK
// request, answered with *iam.Policy, *iam.ValidationResult and
// *iam.TestPolicyResponse.

import (
	"context"
	"reflect"
	"strconv"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const policyServiceName = "quicbaas.IAM.Policy"

// handleIAMPolicyUpdate applies an UpdatePolicyRequest to a stored policy
// through PolicyEngine.UpdatePolicy. Unset fields keep their stored value;
// SpecYAML, when present, replaces the policy's rules.
func handleIAMPolicyUpdate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := iamRequestStructPtr(req, "Request").(*iamsdk.UpdatePolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(policyServiceName, "Update",
			"*iam.UpdatePolicyRequest", reflect.TypeOf(req).String())
	}
	if err := checkNoUntranslatableUpdateFields(sdkReq); err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	policyName, _ := iamRequestField(req, "PolicyName")

	existing, err := pe.GetPolicy(ctx, tenantID, policyName)
	if err != nil {
		return nil, err
	}
	meta := existing.Metadata
	if sdkReq.Description != nil {
		meta.Description = *sdkReq.Description
	}
	if sdkReq.Priority != nil {
		meta.Priority = *sdkReq.Priority
	}
	if sdkReq.Labels != nil {
		meta.Labels = sdkReq.Labels
	}

	policy := &iam.Policy{
		APIVersion: existing.APIVersion,
		Kind:       existing.Kind,
		Metadata:   meta,
		Spec:       existing.Spec,
	}
	// SpecYAML takes precedence over the individual rule fields, as on
	// Create.
	switch {
	case len(sdkReq.SpecYAML) > 0:
		policy, err = iam.ParsePolicyYAML(sdkReq.SpecYAML, meta)
		if err != nil {
			return nil, err
		}
	case sdkReq.Rules != nil:
		policy.Spec.Rules = iamRulesFromSDK(sdkReq.Rules)
	}

	if err := pe.UpdatePolicy(ctx, tenantID, policy); err != nil {
		return nil, err
	}
	out := sdkPolicyFromInternal(policy)
	return &out, nil
}

// checkNoUntranslatableUpdateFields is the Update counterpart of
// checkNoUntranslatableFields: it refuses fields the engine's policy cannot
// store, because dropping them would grant more than the caller asked for.
func checkNoUntranslatableUpdateFields(req *iamsdk.UpdatePolicyRequest) error {
	refuse := func(field string) error {
		return &UnsupportedPolicyFieldError{
			Service: policyServiceName, Method: "Update", Field: field,
		}
	}
	if req.Effect != nil && *req.Effect != iamsdk.EffectDeny {
		return refuse("effect")
	}
	if req.Enabled != nil && !*req.Enabled {
		return refuse("enabled")
	}
	// The engine keeps no policy version, so a version check could not be
	// honoured.
	if req.Version != 0 {
		return refuse("version")
	}
	if len(req.Conditions) > 0 {
		return refuse("conditions")
	}
	if len(req.Targets) > 0 {
		return refuse("targets")
	}
	for i, r := range req.Rules {
		if len(r.Conditions) > 0 {
			return refuse("rules[" + strconv.Itoa(i) + "].conditions")
		}
		if len(r.Subjects) > 0 {
			return refuse("rules[" + strconv.Itoa(i) + "].subjects")
		}
	}
	return nil
}

// handleIAMPolicyValidate checks a policy document without storing it. It
// parses the document with the strict decoder Create uses, checks it against
// the requested name, and runs PolicyEngine.ValidatePolicy. A document that
// fails is reported in the result, as on gRPC; only a request with no
// document is an error.
func handleIAMPolicyValidate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	sdkPolicy, ok := iamRequestStructPtr(req, "Policy").(*iamsdk.Policy)
	if !ok {
		return nil, NewInvalidRequestTypeError(policyServiceName, "Validate",
			"*iam.Policy", reflect.TypeOf(req).String())
	}
	if len(sdkPolicy.SpecYAML) == 0 {
		return nil, errors.NewInvalidInput("embedded."+policyServiceName+".Validate",
			"spec_yaml is required")
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	if tenantID == "" {
		tenantID = sdkPolicy.TenantID
	}
	name, _ := iamRequestField(req, "Name")

	if err := validatePolicyDocument(pe, sdkPolicy.SpecYAML, tenantID, name); err != nil {
		return &iamsdk.ValidationResult{
			Valid:  false,
			Errors: []iamsdk.ValidationError{{Message: err.Error()}},
		}, nil
	}
	return &iamsdk.ValidationResult{Valid: true}, nil
}

// validatePolicyDocument returns the first reason specYAML would not be
// accepted by Create under tenantID and name, or nil. An empty name checks
// nothing.
func validatePolicyDocument(pe *iam.PolicyEngine, specYAML []byte, tenantID, name string) error {
	// Parse with the tenant only, as gRPC does: passing the name here would
	// overwrite the document's metadata.name and hide a mismatch.
	policy, err := iam.ParsePolicyYAML(specYAML, iam.PolicyMetadata{Tenant: tenantID})
	if err != nil {
		return err
	}
	if err := iam.CheckPolicyName(policy, name); err != nil {
		return err
	}
	return pe.ValidatePolicy(policy)
}

// handleIAMPolicyTest evaluates each test case through
// PolicyEngine.TestPolicy against the tenant's stored policies and reports
// whether the decision matched the expected result. Like gRPC, it evaluates
// every stored policy of the tenant and ignores Request.PolicyName: the
// engine decides access from all of a tenant's policies, so a test of one
// policy in isolation would not reflect a real decision.
func handleIAMPolicyTest(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := iamRequestStructPtr(req, "Request").(*iamsdk.TestPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(policyServiceName, "Test",
			"*iam.TestPolicyRequest", reflect.TypeOf(req).String())
	}
	if len(sdkReq.TestCases) == 0 {
		return nil, errors.NewInvalidInput("embedded."+policyServiceName+".Test",
			"at least one test case is required")
	}
	tenantID, _ := iamRequestField(req, "TenantID")

	resp := &iamsdk.TestPolicyResponse{
		Results: make([]iamsdk.TestCaseResult, 0, len(sdkReq.TestCases)),
	}
	for _, tc := range sdkReq.TestCases {
		check := &iam.CheckRequest{
			Subject: iam.Subject{
				ID:         tc.Subject.ID,
				TenantID:   tenantID,
				Roles:      tc.Subject.Roles,
				Groups:     tc.Subject.Groups,
				Attributes: tc.Subject.Attributes,
			},
			Action:   tc.Action,
			Resource: tc.Resource,
			Context:  tc.Context,
		}
		decision, err := pe.TestPolicy(ctx, tenantID, check)
		if err != nil {
			return nil, err
		}
		result := testCaseResult(tc, decision)
		if result.Passed {
			resp.Passed++
		} else {
			resp.Failed++
		}
		resp.Results = append(resp.Results, result)
	}
	return resp, nil
}

// testCaseResult maps one engine decision onto the SDK result. A case passes
// when the decision matches its expected result; the zero Effect is deny.
func testCaseResult(tc iamsdk.TestCase, decision *iam.CheckResponse) iamsdk.TestCaseResult {
	actual := iamsdk.EffectDeny
	if decision.Allowed {
		actual = iamsdk.EffectAllow
	}
	passed := actual == tc.ExpectedResult
	return iamsdk.TestCaseResult{
		Name:             tc.Name,
		Passed:           passed,
		ExpectedResult:   tc.ExpectedResult,
		ActualResult:     actual,
		MatchedRule:      decision.MatchedRule,
		EvaluationTimeNs: decision.EvaluationTimeNs,
	}
}
