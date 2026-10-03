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

package grpc

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// attestationServicePrefix is the gRPC-style service identifier the SDK
// attestation surface routes through. It matches the const used by the
// REST / QUIC / MCP / embedded transports.
const attestationServicePrefix = attestationclient.ServiceName

// isAttestationService reports whether the service name targets the
// quicbaas.Attestation surface so doRequest can branch into the typed
// attestation handler. The SDK attestation surface uses plain Go structs that
// do not implement proto.Message, so they must be converted to the generated
// AttestationService proto types first.
func isAttestationService(svc string) bool {
	return strings.HasPrefix(svc, attestationServicePrefix)
}

// attestationHandler converts between the leaf client/attestation structs and
// the generated AttestationService proto types, then calls the gRPC service
// registered by the server lifecycle.
type attestationHandler struct {
	conn *grpc.ClientConn
}

// newAttestationHandler creates a new attestation handler bound to conn.
func newAttestationHandler(conn *grpc.ClientConn) *attestationHandler {
	return &attestationHandler{conn: conn}
}

// handleRequest routes Attestation requests to the appropriate handler based
// on method name.
func (h *attestationHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	client := pb.NewAttestationServiceClient(h.conn)

	switch method {
	case "Verify":
		return h.handleVerify(ctx, client, req)
	case "VerifyChain":
		return h.handleVerifyChain(ctx, client, req)
	case "AttestContainer":
		return h.handleAttestContainer(ctx, client, req)
	default:
		if verifier, ok := attestationVerifierHandlers[method]; ok {
			return verifier(ctx, client, req)
		}
		return nil, errors.NewUnimplemented("attestationHandler.handleRequest",
			"method not exposed by AttestationService gRPC proto: "+method)
	}
}

func (h *attestationHandler) handleVerify(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*attestationclient.VerifyRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("attestationHandler.handleVerify",
			"invalid request type, expected *attestation.VerifyRequest")
	}
	if sdkReq.Format == "" {
		return nil, errors.NewInvalidInput("attestationHandler.handleVerify", "format is required")
	}
	if len(sdkReq.ChainPEM) == 0 {
		return nil, errors.NewInvalidInput("attestationHandler.handleVerify", "chain is required")
	}

	pbResp, err := client.Verify(ctx, &pb.VerifyAttestationRequest{
		Format:             sdkReq.Format,
		Backend:            sdkReq.Backend,
		ChainPem:           sdkReq.ChainPEM,
		AttestedKeyDer:     sdkReq.AttestedKeyDER,
		AttestationData:    sdkReq.AttestationData,
		Signature:          sdkReq.Signature,
		SignatureAlgorithm: sdkReq.SignatureAlgorithm,
		Nonce:              sdkReq.Nonce,
		CreatedAt:          sdkReq.CreatedAt,
		Pcrs:               sdkReq.PCRs,
		Options:            attestationOptionsToProto(sdkReq.Options),
	})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.handleVerify")
	}

	return &attestationclient.VerifyResponse{
		Valid:          pbResp.Valid,
		Format:         pbResp.Format,
		Backend:        pbResp.Backend,
		ChainValid:     pbResp.ChainValid,
		SignatureValid: pbResp.SignatureValid,
		FreshnessValid: pbResp.FreshnessValid,
		PCRsValid:      pbResp.PcrsValid,
		Reason:         pbResp.Reason,
	}, nil
}

func (h *attestationHandler) handleVerifyChain(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*attestationclient.VerifyChainRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("attestationHandler.handleVerifyChain",
			"invalid request type, expected *attestation.VerifyChainRequest")
	}
	if len(sdkReq.ChainPEM) == 0 {
		return nil, errors.NewInvalidInput("attestationHandler.handleVerifyChain", "chain is required")
	}

	pbResp, err := client.VerifyChain(ctx, &pb.VerifyAttestationChainRequest{
		Format:   sdkReq.Format,
		ChainPem: sdkReq.ChainPEM,
		Options:  attestationOptionsToProto(sdkReq.Options),
	})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.handleVerifyChain")
	}

	return &attestationclient.VerifyChainResponse{
		Valid:  pbResp.Valid,
		Reason: pbResp.Reason,
	}, nil
}

func (h *attestationHandler) handleAttestContainer(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*attestationclient.AttestContainerRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("attestationHandler.handleAttestContainer",
			"invalid request type, expected *attestation.AttestContainerRequest")
	}
	if sdkReq.Image == "" {
		return nil, errors.NewInvalidInput("attestationHandler.handleAttestContainer", "image is required")
	}

	pbResp, err := client.AttestContainer(ctx, &pb.AttestContainerRequest{
		DeviceId:        sdkReq.DeviceID,
		ContainerId:     sdkReq.ContainerID,
		Image:           sdkReq.Image,
		ImageDigest:     sdkReq.ImageDigest,
		TrustLabel:      sdkReq.TrustLabel,
		TrustLevel:      sdkReq.TrustLevel,
		ExpectedPcrs:    sdkReq.ExpectedPCRs,
		MeasurementPcr:  int32(sdkReq.MeasurementPCR),
		MeasurementBank: sdkReq.MeasurementBank,
		Nonce:           sdkReq.Nonce,
	})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.handleAttestContainer")
	}

	resp := &attestationclient.AttestContainerResponse{
		Attested:          pbResp.Attested,
		DeviceID:          pbResp.DeviceId,
		ContainerID:       pbResp.ContainerId,
		Image:             pbResp.Image,
		ImageDigest:       pbResp.ImageDigest,
		ConfigDigest:      pbResp.ConfigDigest,
		TrustLabel:        pbResp.TrustLabel,
		TrustLevel:        pbResp.TrustLevel,
		Nonce:             pbResp.Nonce,
		Quoted:            pbResp.Quoted,
		Signature:         pbResp.Signature,
		PCRs:              pbResp.Pcrs,
		EventLog:          pbResp.EventLog,
		Measured:          pbResp.Measured,
		MeasurementPCR:    int(pbResp.MeasurementPcr),
		MeasurementBank:   pbResp.MeasurementBank,
		LaunchMeasurement: pbResp.LaunchMeasurement,
	}
	if pbResp.AttestedAt != nil {
		resp.AttestedAt = pbResp.AttestedAt.AsTime().UTC().Format(time.RFC3339Nano)
	}
	return resp, nil
}

// attestationOptionsToProto converts the leaf verify options into their proto
// form. A nil input stays nil so the server applies the xkms secure defaults.
func attestationOptionsToProto(opts *attestationclient.VerifyOptions) *pb.AttestationVerifyOptions {
	if opts == nil {
		return nil
	}
	return &pb.AttestationVerifyOptions{
		TrustedRootsPem:        opts.TrustedRootsPEM,
		CheckFreshness:         opts.CheckFreshness,
		ExpectedNonce:          opts.ExpectedNonce,
		FreshnessWindowSeconds: opts.FreshnessWindowSeconds,
		VerifyPcrs:             opts.VerifyPCRs,
		ExpectedPcrs:           opts.ExpectedPCRs,
		AllowSelfSigned:        opts.AllowSelfSigned,
	}
}
