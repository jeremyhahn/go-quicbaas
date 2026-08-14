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

// certificate_extensions_handler.go converts the leaf client/certificate
// extension wire types to and from the generated CertificateService proto
// types for CrossSign, Chain and IssueDeviceIdentity. These three verbs back
// Security().CertificateExt() and are also what the Unix transport serves,
// since it delegates its entire Request path to this gRPC transport.

package grpc

import (
	"context"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleCrossSign cross-signs an external root certificate with the server's
// CA.
func (h *certificateHandler) handleCrossSign(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.CrossSignRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("certificateHandler.handleCrossSign",
			"invalid request type, expected *certificate.CrossSignRequest")
	}
	if len(sdkReq.ForeignRootPEM) == 0 {
		return nil, errors.NewInvalidInput("certificateHandler.handleCrossSign",
			"foreign root PEM is required")
	}

	pbResp, err := client.CrossSign(ctx, &pb.CrossSignRequest{
		ForeignRootPem: sdkReq.ForeignRootPEM,
	})
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleCrossSign")
	}

	return &certificate.CrossSignResponse{CrossSignedPEM: pbResp.CrossSignedPem}, nil
}

// handleChain resolves the issuer chain for a certificate.
func (h *certificateHandler) handleChain(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.ChainRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("certificateHandler.handleChain",
			"invalid request type, expected *certificate.ChainRequest")
	}
	if len(sdkReq.CertificatePEM) == 0 {
		return nil, errors.NewInvalidInput("certificateHandler.handleChain",
			"certificate PEM is required")
	}

	pbResp, err := client.Chain(ctx, &pb.CertificateChainRequest{
		CertificatePem: sdkReq.CertificatePEM,
	})
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleChain")
	}

	return &certificate.ChainResponse{ChainPEM: pbResp.ChainPem}, nil
}

// handleIssueDeviceIdentity issues a TCG device identity certificate.
func (h *certificateHandler) handleIssueDeviceIdentity(ctx context.Context, client pb.CertificateServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*certificate.DeviceIdentityRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("certificateHandler.handleIssueDeviceIdentity",
			"invalid request type, expected *certificate.DeviceIdentityRequest")
	}
	if sdkReq.Type == "" {
		return nil, errors.NewInvalidInput("certificateHandler.handleIssueDeviceIdentity",
			"identity type is required")
	}
	if sdkReq.DeviceID == "" {
		return nil, errors.NewInvalidInput("certificateHandler.handleIssueDeviceIdentity",
			"device ID is required")
	}

	pbResp, err := client.IssueDeviceIdentity(ctx, &pb.IssueDeviceIdentityRequest{
		Type:           string(sdkReq.Type),
		DeviceId:       sdkReq.DeviceID,
		CsrPem:         sdkReq.CSRPEM,
		EkPub:          sdkReq.EKPub,
		AkPub:          sdkReq.AKPub,
		QualifyingData: sdkReq.QualifyingData,
		ValidityDays:   int32(sdkReq.ValidityDays),
		TenantId:       sdkReq.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "certificateHandler.handleIssueDeviceIdentity")
	}

	resp := &certificate.DeviceIdentityResponse{
		CertificatePEM: pbResp.CertificatePem,
		ChainPEM:       pbResp.ChainPem,
		SerialNumber:   pbResp.SerialNumber,
	}
	if pbResp.NotBefore != nil {
		resp.NotBefore = pbResp.NotBefore.AsTime()
	}
	if pbResp.NotAfter != nil {
		resp.NotAfter = pbResp.NotAfter.AsTime()
	}
	return resp, nil
}
