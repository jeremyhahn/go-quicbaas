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

	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// ACME Handlers
// =============================================================================
//
// The ACME service composes the go-xkms RFC 8555 server. These embedded
// handlers delegate to the service-layer ACMEServicer and return the shared
// client/acme wire types the SDK acmeServerImpl type-asserts on.
//
// Only the server-side CONTROL verbs (RegisterChallenge, Issue) are wired:
// they round-trip correctly in-process. The protocol-client verbs
// (NewOrder / FinalizeOrder / Renew) are JWS-signed RFC 8555 exchanges driven
// by an ACME protocol client against the mounted directory, not quicbaas
// transport RPCs, so they have no embedded handler by design.

func handleACMERegisterChallenge(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	acmeSvc := svc.ACME()
	if acmeSvc == nil {
		return nil, NewServiceNotConfiguredError("ACME")
	}

	sdkReq, ok := req.(*acmeclient.RegisterChallengeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ACME", "RegisterChallenge",
			"*acme.RegisterChallengeRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := acmeSvc.RegisterChallenge(ctx, &service.ACMERegisterChallengeRequest{
		Name: sdkReq.Name,
	})
	if err != nil {
		return nil, err
	}
	return &acmeclient.RegisterChallengeResponse{Registered: svcResp.Registered}, nil
}

func handleACMEIssue(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	acmeSvc := svc.ACME()
	if acmeSvc == nil {
		return nil, NewServiceNotConfiguredError("ACME")
	}

	sdkReq, ok := req.(*acmeclient.IssueRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ACME", "Issue",
			"*acme.IssueRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := acmeSvc.Issue(ctx, &service.ACMEIssueRequest{
		CSRPEM:         sdkReq.CSRPEM,
		ChallengeType:  sdkReq.ChallengeType,
		ChallengeValue: sdkReq.ChallengeValue,
	})
	if err != nil {
		return nil, err
	}
	return &acmeclient.IssueResponse{
		CertificatePEM: svcResp.CertificatePEM,
		ChainPEM:       svcResp.ChainPEM,
		SerialNumber:   svcResp.SerialNumber,
	}, nil
}
