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

// The embedded IAM Policy GetEffectivePermissions verb (go-quicbaas#1568).
// It calls the policy engine's GetEffectivePermissions, the evaluation the
// REST route serves. The client's ResourceType is the one resource pattern
// and its ActionPattern the one action; an empty ActionPattern lets the
// engine check its default actions.

import (
	"context"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// handleIAMPolicyGetEffectivePermissions evaluates the subject's actions on
// the request's ResourceType in the target tenant through the policy engine,
// and answers the allowed actions (and, when IncludeDenied is set, the
// denied ones) as SDK effective permissions.
func handleIAMPolicyGetEffectivePermissions(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pe, err := policyEngineFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := iamRequestStructPtr(req, "Request").(*iamsdk.EffectivePermissionsRequest)
	if !ok || sdkReq == nil {
		return nil, NewInvalidRequestTypeError(policyServiceName, "GetEffectivePermissions",
			"*iam.EffectivePermissionsRequest", typeNameOf(req))
	}
	requested, _ := iamRequestField(req, "TenantID")
	tenantID, err := tenantctx.Target(ctx, policyServiceName+"/GetEffectivePermissions", requested)
	if err != nil {
		return nil, err
	}

	op := "embedded." + policyServiceName + ".GetEffectivePermissions"
	if sdkReq.Subject.ID == "" {
		return nil, errors.NewInvalidInput(op, "subject ID is required")
	}
	if sdkReq.ResourceType == "" {
		return nil, errors.NewInvalidInput(op, "resource type is required")
	}

	engineReq := &iam.EffectivePermissionsRequest{
		Subject: &iam.Subject{
			ID:         sdkReq.Subject.ID,
			TenantID:   tenantID,
			Roles:      sdkReq.Subject.Roles,
			Groups:     sdkReq.Subject.Groups,
			Attributes: sdkReq.Subject.Attributes,
		},
		ResourcePatterns:   []string{sdkReq.ResourceType},
		IncludeDenyDetails: sdkReq.IncludeDenied,
	}
	if sdkReq.ActionPattern != "" {
		engineReq.Actions = []string{sdkReq.ActionPattern}
	}
	result, err := pe.GetEffectivePermissions(ctx, tenantID, engineReq)
	if err != nil {
		return nil, err
	}

	resp := &iamsdk.EffectivePermissionsResponse{
		Permissions:      make([]iamsdk.EffectivePermission, 0),
		EvaluationTimeNs: result.EvaluationTimeNs,
	}
	set := result.Permissions[sdkReq.ResourceType]
	if set == nil {
		return resp, nil
	}
	for _, action := range set.Allowed {
		resp.Permissions = append(resp.Permissions, iamsdk.EffectivePermission{
			Action:          action,
			ResourcePattern: sdkReq.ResourceType,
			Effect:          iamsdk.EffectAllow,
		})
	}
	if sdkReq.IncludeDenied {
		for _, action := range set.Denied {
			resp.Permissions = append(resp.Permissions, iamsdk.EffectivePermission{
				Action:          action,
				ResourcePattern: sdkReq.ResourceType,
				Effect:          iamsdk.EffectDeny,
				SourcePolicies:  set.Sources[action],
			})
		}
	}
	return resp, nil
}
