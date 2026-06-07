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

package embedded

import (
	"context"
	"reflect"

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Trust Handlers
// =============================================================================
//
// The Trust service composes the xkms TrustedCA cross-sign / AIA verbs.
// These embedded handlers delegate to the service-layer TrustService and
// return the leaf client/trust wire types the SDK trustServiceImpl
// type-asserts on. PublishDANE is intentionally NOT wired here (it is
// pending a separate DNS-provider hook design).

func handleTrustCrossSignedRootPool(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	trustSvc := svc.Trust()
	if trustSvc == nil {
		return nil, NewServiceNotConfiguredError("Trust")
	}

	// Accept the SDK client request (carries no fields) or a nil request.
	if req != nil {
		if _, ok := req.(*trustclient.CrossSignedRootPoolRequest); !ok {
			return nil, NewInvalidRequestTypeError(
				"quicbaas.Trust", "CrossSignedRootPool",
				"*trust.CrossSignedRootPoolRequest",
				reflect.TypeOf(req).String(),
			)
		}
	}

	svcResp, err := trustSvc.CrossSignedRootPool(ctx, &service.CrossSignedRootPoolRequest{})
	if err != nil {
		return nil, err
	}
	return &trustclient.CrossSignedRootPoolResponse{RootPEMs: svcResp.RootPEMs}, nil
}

func handleTrustAutoImportAIA(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	trustSvc := svc.Trust()
	if trustSvc == nil {
		return nil, NewServiceNotConfiguredError("Trust")
	}

	sdkReq, ok := req.(*trustclient.AutoImportAIARequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Trust", "AutoImportAIA",
			"*trust.AutoImportAIARequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := trustSvc.AutoImportAIA(ctx, &service.AutoImportAIARequest{
		ChainDER: sdkReq.ChainDER,
	})
	if err != nil {
		return nil, err
	}
	return &trustclient.AutoImportAIAResponse{Imported: svcResp.Imported}, nil
}

func handleTrustPublishDANE(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	trustSvc := svc.Trust()
	if trustSvc == nil {
		return nil, NewServiceNotConfiguredError("Trust")
	}

	sdkReq, ok := req.(*trustclient.PublishDANERequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Trust", "PublishDANE",
			"*trust.PublishDANERequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := trustSvc.PublishDANE(ctx, &service.PublishDANERequest{
		Name:    sdkReq.Name,
		Port:    sdkReq.Port,
		CertDER: sdkReq.CertDER,
	})
	if err != nil {
		return nil, err
	}
	return &trustclient.PublishDANEResponse{Published: svcResp.Published}, nil
}
