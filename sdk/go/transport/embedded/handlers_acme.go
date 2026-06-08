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
// client/acme wire types the SDK acmeServerImpl / acmeClientImpl type-assert
// on.
//
// Both the server-side CONTROL verbs (RegisterChallenge, Issue) and the
// broker-as-CA protocol-CLIENT verbs (NewOrder, FinalizeOrder, Renew) are
// wired: in the broker-as-CA model QuicBaaS is itself the certificate
// authority, so the client verbs are in-process service calls rather than
// JWS-signed RFC 8555 exchanges.

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

func handleACMENewOrder(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	acmeSvc := svc.ACME()
	if acmeSvc == nil {
		return nil, NewServiceNotConfiguredError("ACME")
	}

	sdkReq, ok := req.(*acmeclient.NewOrderRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ACME", "NewOrder",
			"*acme.NewOrderRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := acmeSvc.NewOrder(ctx, &service.ACMENewOrderRequest{
		Identifiers:   acmeToServiceIdentifiers(sdkReq.Identifiers),
		ChallengeType: sdkReq.ChallengeType,
	})
	if err != nil {
		return nil, err
	}
	return &acmeclient.NewOrderResponse{
		OrderID:     svcResp.OrderID,
		Status:      svcResp.Status,
		Identifiers: acmeFromServiceIdentifiers(svcResp.Identifiers),
		Finalize:    svcResp.Finalize,
		URL:         svcResp.URL,
	}, nil
}

func handleACMEFinalizeOrder(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	acmeSvc := svc.ACME()
	if acmeSvc == nil {
		return nil, NewServiceNotConfiguredError("ACME")
	}

	sdkReq, ok := req.(*acmeclient.FinalizeOrderRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ACME", "FinalizeOrder",
			"*acme.FinalizeOrderRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := acmeSvc.FinalizeOrder(ctx, &service.ACMEFinalizeOrderRequest{
		OrderURL: sdkReq.OrderURL,
		CSRPEM:   sdkReq.CSRPEM,
	})
	if err != nil {
		return nil, err
	}
	return &acmeclient.FinalizeOrderResponse{
		CertificatePEM: svcResp.CertificatePEM,
		ChainPEM:       svcResp.ChainPEM,
		SerialNumber:   svcResp.SerialNumber,
	}, nil
}

func handleACMERenew(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	acmeSvc := svc.ACME()
	if acmeSvc == nil {
		return nil, NewServiceNotConfiguredError("ACME")
	}

	sdkReq, ok := req.(*acmeclient.RenewRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ACME", "Renew",
			"*acme.RenewRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcResp, err := acmeSvc.Renew(ctx, &service.ACMERenewRequest{
		CertURL: sdkReq.CertURL,
		Revoke:  sdkReq.Revoke,
	})
	if err != nil {
		return nil, err
	}
	return &acmeclient.RenewResponse{
		CertificatePEM: svcResp.CertificatePEM,
		SerialNumber:   svcResp.SerialNumber,
		Revoked:        svcResp.Revoked,
		Renewable:      svcResp.Renewable,
	}, nil
}

// acmeToServiceIdentifiers maps wire identifiers to service identifiers.
func acmeToServiceIdentifiers(in []acmeclient.OrderIdentifier) []service.ACMEOrderIdentifier {
	out := make([]service.ACMEOrderIdentifier, len(in))
	for i, id := range in {
		out[i] = service.ACMEOrderIdentifier{Type: id.Type, Value: id.Value}
	}
	return out
}

// acmeFromServiceIdentifiers maps service identifiers back to wire identifiers.
func acmeFromServiceIdentifiers(in []service.ACMEOrderIdentifier) []acmeclient.OrderIdentifier {
	out := make([]acmeclient.OrderIdentifier, len(in))
	for i, id := range in {
		out[i] = acmeclient.OrderIdentifier{Type: id.Type, Value: id.Value}
	}
	return out
}
