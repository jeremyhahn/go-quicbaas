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

// Policy Simulate and PEP CheckBatch handlers (go-quicbaas#1564).
//
// Both route to the policy engine the other transports already call: the
// gRPC and REST transports map Simulate onto the TestPolicy RPC, which
// evaluates the check through PolicyEngine, and the gRPC PEP server's
// CheckBatch evaluates each resource through PolicyEngine.Evaluate. The
// decision itself is go-qrdb's; these handlers only translate shapes.

import (
	"context"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const pepServiceName = "quicbaas.IAM.PEP"

// handleIAMPolicySimulate evaluates the client's SimulateRequest in the
// tenant the call is bound to, as the gRPC TestPolicy RPC does. A request
// naming another tenant is refused before the engine is called.
func handleIAMPolicySimulate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := iamRequestStructPtr(req, "Request").(*iamsdk.SimulateRequest)
	if !ok || sdkReq == nil {
		return nil, NewInvalidRequestTypeError(policyServiceName, "Simulate",
			"*iam.SimulateRequest", typeNameOf(req))
	}
	requested, _ := iamRequestField(req, "TenantID")
	tenantID, err := tenantctx.Target(ctx, policyServiceName+"/Simulate", requested)
	if err != nil {
		return nil, err
	}

	check := &iam.CheckRequest{
		Subject: iam.Subject{
			ID:         sdkReq.Subject.ID,
			TenantID:   tenantID,
			Roles:      sdkReq.Subject.Roles,
			Groups:     sdkReq.Subject.Groups,
			Attributes: sdkReq.Subject.Attributes,
		},
		Action:   sdkReq.Action,
		Resource: sdkReq.Resource,
		Context:  sdkReq.Context,
	}
	decision, err := pe.TestPolicy(ctx, tenantID, check)
	if err != nil {
		return nil, err
	}

	resp := &iamsdk.SimulateResponse{
		Allowed:          decision.Allowed,
		Effect:           iamsdk.EffectDeny,
		EvaluationTimeNs: decision.EvaluationTimeNs,
	}
	if decision.Allowed {
		resp.Effect = iamsdk.EffectAllow
	}
	if decision.MatchedPolicy != "" {
		resp.MatchedPolicies = []string{decision.MatchedPolicy}
	}
	if decision.MatchedRule != "" {
		resp.MatchedRules = []string{decision.MatchedRule}
	}
	return resp, nil
}

// handleIAMPEPCheckBatch evaluates every check of the batch through the
// policy engine, in request order, stopping after the first denial when the
// batch sets StopOnDeny. Each check acts in the tenant the call is
// bound to; a check naming another tenant refuses the whole batch before
// any check is evaluated.
func handleIAMPEPCheckBatch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	batch, ok := iamRequestStructPtr(req, "Request").(*iamsdk.CheckBatchRequest)
	if !ok || batch == nil {
		return nil, NewInvalidRequestTypeError(pepServiceName, "CheckBatch",
			"*iam.CheckBatchRequest", typeNameOf(req))
	}

	checks := make([]*iam.CheckRequest, 0, len(batch.Requests))
	for _, r := range batch.Requests {
		requested := r.TenantID
		if requested == "" {
			requested = r.Subject.TenantID
		}
		tenantID, err := tenantctx.Target(ctx, pepServiceName+"/CheckBatch", requested)
		if err != nil {
			return nil, err
		}
		checks = append(checks, &iam.CheckRequest{
			Subject: iam.Subject{
				ID:         r.Subject.ID,
				TenantID:   tenantID,
				Roles:      r.Subject.Roles,
				Groups:     r.Subject.Groups,
				Attributes: r.Subject.Attributes,
			},
			Action:   r.Action,
			Resource: r.Resource,
			Context:  r.Context,
		})
	}

	resp := &iamsdk.CheckBatchResponse{
		Results:    make([]iamsdk.CheckResponse, 0, len(checks)),
		AllAllowed: true,
	}
	for _, check := range checks {
		decision, err := pe.Evaluate(ctx, check)
		if err != nil {
			return nil, err
		}
		result := iamsdk.CheckResponse{
			Allowed:          decision.Allowed,
			Effect:           iamsdk.EffectDeny,
			MatchedPolicy:    decision.MatchedPolicy,
			MatchedRule:      decision.MatchedRule,
			EvaluationTimeNs: decision.EvaluationTimeNs,
			Reason:           decision.Reason,
		}
		if decision.Allowed {
			result.Effect = iamsdk.EffectAllow
			resp.AllowedCount++
		} else {
			resp.AllAllowed = false
			resp.DeniedCount++
		}
		resp.TotalEvaluationTimeNs += decision.EvaluationTimeNs
		resp.Results = append(resp.Results, result)
		if batch.StopOnDeny && !decision.Allowed {
			break
		}
	}
	return resp, nil
}
