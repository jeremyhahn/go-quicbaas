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
	"time"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Attestation Handlers
// =============================================================================
//
// The Attestation service composes the go-xkms attestation verifier and the
// container-attestation orchestration seam. These embedded handlers delegate
// to the service-layer AttestationService and return the leaf
// client/attestation wire types the SDK attestationServiceImpl type-asserts
// on.

func handleAttestationVerify(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	attestationSvc := svc.Attestation()
	if attestationSvc == nil {
		return nil, NewServiceNotConfiguredError("Attestation")
	}

	sdkReq, ok := req.(*attestationclient.VerifyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Attestation", "Verify",
			"*attestation.VerifyRequest",
			typeNameOf(req),
		)
	}

	svcResp, err := attestationSvc.Verify(ctx, &service.VerifyAttestationRequest{
		Format:             sdkReq.Format,
		Backend:            sdkReq.Backend,
		ChainPEM:           sdkReq.ChainPEM,
		AttestedKeyDER:     sdkReq.AttestedKeyDER,
		AttestationData:    sdkReq.AttestationData,
		Signature:          sdkReq.Signature,
		SignatureAlgorithm: sdkReq.SignatureAlgorithm,
		Nonce:              sdkReq.Nonce,
		CreatedAt:          sdkReq.CreatedAt,
		PCRs:               sdkReq.PCRs,
		Options:            serviceVerifyOptions(sdkReq.Options),
	})
	if err != nil {
		return nil, err
	}

	return &attestationclient.VerifyResponse{
		Valid:          svcResp.Valid,
		Format:         svcResp.Format,
		Backend:        svcResp.Backend,
		ChainValid:     svcResp.ChainValid,
		SignatureValid: svcResp.SignatureValid,
		FreshnessValid: svcResp.FreshnessValid,
		PCRsValid:      svcResp.PCRsValid,
		Reason:         svcResp.Reason,
	}, nil
}

func handleAttestationVerifyChain(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	attestationSvc := svc.Attestation()
	if attestationSvc == nil {
		return nil, NewServiceNotConfiguredError("Attestation")
	}

	sdkReq, ok := req.(*attestationclient.VerifyChainRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Attestation", "VerifyChain",
			"*attestation.VerifyChainRequest",
			typeNameOf(req),
		)
	}

	svcResp, err := attestationSvc.VerifyChain(ctx, &service.VerifyAttestationChainRequest{
		Format:   sdkReq.Format,
		ChainPEM: sdkReq.ChainPEM,
		Options:  serviceVerifyOptions(sdkReq.Options),
	})
	if err != nil {
		return nil, err
	}

	return &attestationclient.VerifyChainResponse{
		Valid:  svcResp.Valid,
		Reason: svcResp.Reason,
	}, nil
}

func handleAttestationAttestContainer(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	attestationSvc := svc.Attestation()
	if attestationSvc == nil {
		return nil, NewServiceNotConfiguredError("Attestation")
	}

	sdkReq, ok := req.(*attestationclient.AttestContainerRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Attestation", "AttestContainer",
			"*attestation.AttestContainerRequest",
			typeNameOf(req),
		)
	}

	svcResp, err := attestationSvc.AttestContainer(ctx, &service.AttestContainerRequest{
		DeviceID:        sdkReq.DeviceID,
		ContainerID:     sdkReq.ContainerID,
		Image:           sdkReq.Image,
		ImageDigest:     sdkReq.ImageDigest,
		TrustLabel:      sdkReq.TrustLabel,
		TrustLevel:      sdkReq.TrustLevel,
		ReferenceValues: sdkReq.ReferenceValues,
		MeasurementPCR:  sdkReq.MeasurementPCR,
		MeasurementBank: sdkReq.MeasurementBank,
		Nonce:           sdkReq.Nonce,
	})
	if err != nil {
		return nil, err
	}

	return attestContainerWireResponse(svcResp), nil
}

// attestContainerWireResponse converts the service-layer evidence into the
// leaf wire type, rendering the timestamp as RFC 3339 so the shape matches
// every other transport.
func attestContainerWireResponse(svcResp *service.AttestContainerResponse) *attestationclient.AttestContainerResponse {
	resp := &attestationclient.AttestContainerResponse{
		Attested:          svcResp.Attested,
		DeviceID:          svcResp.DeviceID,
		ContainerID:       svcResp.ContainerID,
		Image:             svcResp.Image,
		ImageDigest:       svcResp.ImageDigest,
		ConfigDigest:      svcResp.ConfigDigest,
		TrustLabel:        svcResp.TrustLabel,
		TrustLevel:        svcResp.TrustLevel,
		Nonce:             svcResp.Nonce,
		Quoted:            svcResp.Quoted,
		Signature:         svcResp.Signature,
		PCRs:              svcResp.PCRs,
		EventLog:          svcResp.EventLog,
		Measured:          svcResp.Measured,
		MeasurementPCR:    svcResp.MeasurementPCR,
		MeasurementBank:   svcResp.MeasurementBank,
		LaunchMeasurement: svcResp.LaunchMeasurement,
	}
	if !svcResp.AttestedAt.IsZero() {
		resp.AttestedAt = svcResp.AttestedAt.UTC().Format(time.RFC3339Nano)
	}
	return resp
}

// serviceVerifyOptions converts the leaf verify options into the
// service-layer form. A nil input stays nil so the service applies the xkms
// secure defaults rather than an all-false struct.
func serviceVerifyOptions(opts *attestationclient.VerifyOptions) *service.VerifyAttestationOptions {
	if opts == nil {
		return nil
	}
	return &service.VerifyAttestationOptions{
		TrustedRootsPEM:        opts.TrustedRootsPEM,
		CheckFreshness:         opts.CheckFreshness,
		ExpectedNonce:          opts.ExpectedNonce,
		FreshnessWindowSeconds: opts.FreshnessWindowSeconds,
		VerifyPCRs:             opts.VerifyPCRs,
		ExpectedPCRs:           opts.ExpectedPCRs,
		AllowSelfSigned:        opts.AllowSelfSigned,
	}
}

// typeNameOf renders the dynamic type of a request for an error message,
// tolerating a nil interface (reflect.TypeOf returns nil for one, and calling
// String on that nil would panic).
func typeNameOf(req interface{}) string {
	t := reflect.TypeOf(req)
	if t == nil {
		return "<nil>"
	}
	return t.String()
}
