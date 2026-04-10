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

package grpc

import (
	"context"
	"errors"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Typed errors for PIV gRPC operations in QuicBaaS.
var (
	// ErrXKMSPIVBackendRequired is returned when the backend field is missing
	// from a PIV request.
	ErrXKMSPIVBackendRequired = errors.New("xkms: piv backend is required")

	// ErrXKMSPIVSlotRequired is returned when the slot field is missing
	// from a PIV request.
	ErrXKMSPIVSlotRequired = errors.New("xkms: piv slot is required")

	// ErrXKMSPIVFormatRequired is returned when the format field is missing
	// from a PIV certificate request.
	ErrXKMSPIVFormatRequired = errors.New("xkms: piv certificate format is required")

	// ErrXKMSPIVCertificateRequired is returned when the certificate data is
	// missing from a PIV store/import request.
	ErrXKMSPIVCertificateRequired = errors.New("xkms: piv certificate data is required")
)

// xkmsPIVErrorMap maps PIV domain errors to gRPC status codes.
// Both the SDK-level sentinel errors and the PIV manager's own sentinel errors
// are included because the SDK re-exports generic keystore errors (e.g.,
// xkms.ErrNotInitialized) while the PIV manager returns its own distinct
// sentinel errors (e.g., xkms.ErrPIVNotInitialized).
var xkmsPIVErrorMap = map[error]codes.Code{
	// SDK-level sentinel errors (generic keystore errors re-exported by the SDK).
	xkmssdk.ServiceErrPIVNotInitialized:   codes.Unavailable,
	xkmssdk.ServiceErrPIVBackendNotFound:  codes.NotFound,
	xkmssdk.ServiceErrPIVInvalidAlgorithm: codes.InvalidArgument,
	xkmssdk.ServiceErrPIVInvalidSlot:      codes.InvalidArgument,
	xkmssdk.ServiceErrPIVInvalidFormat:    codes.InvalidArgument,
	xkmssdk.ServiceErrPIVKeyNotFound:      codes.NotFound,

	// PIV manager sentinel errors (the actual errors returned by piv_manager.go).
	xkmssdk.ServiceErrPIVNotInitialized:   codes.Unavailable,
	xkmssdk.ServiceErrPIVBackendNotFound:  codes.NotFound,
	xkmssdk.ServiceErrPIVInvalidAlgorithm: codes.InvalidArgument,
	xkmssdk.ServiceErrPIVInvalidSlot:      codes.InvalidArgument,
	xkmssdk.ServiceErrPIVInvalidFormat:    codes.InvalidArgument,
	xkmssdk.ServiceErrPIVKeyNotFound:      codes.NotFound,
}

// xkmsMapPIVError converts a PIV domain error to the appropriate gRPC status
// error. If the error is a known PIV error, it maps to the corresponding gRPC
// code. Otherwise, it returns an Internal error.
func xkmsMapPIVError(err error, operation string) error {
	if err == nil {
		return nil
	}
	for pivErr, code := range xkmsPIVErrorMap {
		if errors.Is(err, pivErr) {
			return status.Errorf(code, "%s: %v", operation, err)
		}
	}
	return status.Errorf(codes.Internal, "%s: %v", operation, err)
}

// ListPIVSlots returns the status of all PIV slots for the specified backend.
func (s *XKMSServer) ListPIVSlots(ctx context.Context, req *xkmssdk.ProtoListPIVSlotsRequest) (*xkmssdk.ProtoListPIVSlotsResponse, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}

	tReq := &transportTypes.ListPIVSlotsRequest{
		Backend: req.GetBackend(),
	}

	resp, err := xkmssdk.ServiceListPIVSlots(ctx, tReq)
	if err != nil {
		return nil, xkmsMapPIVError(err, "list piv slots")
	}

	pbSlots := make([]*xkmssdk.ProtoPIVSlotStatus, len(resp.Slots))
	for i, slot := range resp.Slots {
		pbSlots[i] = &xkmssdk.ProtoPIVSlotStatus{
			Slot:        slot.Slot,
			Name:        slot.Name,
			Description: slot.Description,
			HasCert:     slot.HasCert,
			Subject:     slot.Subject,
			Algorithm:   slot.Algorithm,
			KeySize:     int32(slot.KeySize), // #nosec G115
			NotAfter:    slot.NotAfter,
			Fingerprint: slot.Fingerprint,
		}
	}

	return &xkmssdk.ProtoListPIVSlotsResponse{
		Slots: pbSlots,
	}, nil
}

// GetPIVCertificate retrieves a certificate from a PIV slot.
func (s *XKMSServer) GetPIVCertificate(ctx context.Context, req *xkmssdk.ProtoGetPIVCertificateRequest) (*xkmssdk.ProtoGetPIVCertificateResponse, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}
	if req.GetFormat() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVFormatRequired.Error())
	}

	tReq := &transportTypes.GetPIVCertificateRequest{
		Backend: req.GetBackend(),
		Slot:    req.GetSlot(),
		Format:  req.GetFormat(),
	}

	resp, err := xkmssdk.ServiceGetPIVCertificate(ctx, tReq)
	if err != nil {
		return nil, xkmsMapPIVError(err, "get piv certificate")
	}

	return &xkmssdk.ProtoGetPIVCertificateResponse{
		Slot:        resp.Slot,
		Certificate: resp.Certificate,
		Format:      resp.Format,
	}, nil
}

// StorePIVCertificate stores a certificate in a PIV slot.
func (s *XKMSServer) StorePIVCertificate(ctx context.Context, req *xkmssdk.ProtoStorePIVCertificateRequest) (*emptypb.Empty, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}
	if req.GetFormat() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVFormatRequired.Error())
	}
	if len(req.GetCertificate()) == 0 {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVCertificateRequired.Error())
	}

	tReq := &transportTypes.StorePIVCertificateRequest{
		Backend:     req.GetBackend(),
		Slot:        req.GetSlot(),
		Certificate: req.GetCertificate(),
		Format:      req.GetFormat(),
	}

	if err := xkmssdk.ServiceStorePIVCertificate(ctx, tReq); err != nil {
		return nil, xkmsMapPIVError(err, "store piv certificate")
	}

	return &emptypb.Empty{}, nil
}

// DeletePIVCertificate removes a certificate from a PIV slot.
func (s *XKMSServer) DeletePIVCertificate(ctx context.Context, req *xkmssdk.ProtoDeletePIVCertificateRequest) (*emptypb.Empty, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}

	tReq := &transportTypes.DeletePIVCertificateRequest{
		Backend: req.GetBackend(),
		Slot:    req.GetSlot(),
	}

	if err := xkmssdk.ServiceDeletePIVCertificate(ctx, tReq); err != nil {
		return nil, xkmsMapPIVError(err, "delete piv certificate")
	}

	return &emptypb.Empty{}, nil
}

// GeneratePIVKey generates a new key pair in a PIV slot with a self-signed certificate.
func (s *XKMSServer) GeneratePIVKey(ctx context.Context, req *xkmssdk.ProtoGeneratePIVKeyRequest) (*xkmssdk.ProtoGeneratePIVKeyResponse, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}

	tReq := &transportTypes.GeneratePIVKeyRequest{
		Backend:   req.GetBackend(),
		Slot:      req.GetSlot(),
		Algorithm: req.GetAlgorithm(),
		Subject:   req.GetSubject(),
	}

	resp, err := xkmssdk.ServiceGeneratePIVKey(ctx, tReq)
	if err != nil {
		return nil, xkmsMapPIVError(err, "generate piv key")
	}

	return &xkmssdk.ProtoGeneratePIVKeyResponse{
		Slot:        resp.Slot,
		Certificate: resp.Certificate,
		PublicKey:   resp.PublicKey,
	}, nil
}

// ImportPIVCertificate imports a certificate into a PIV slot.
func (s *XKMSServer) ImportPIVCertificate(ctx context.Context, req *xkmssdk.ProtoStorePIVCertificateRequest) (*emptypb.Empty, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}
	if req.GetFormat() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVFormatRequired.Error())
	}
	if len(req.GetCertificate()) == 0 {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVCertificateRequired.Error())
	}

	tReq := &transportTypes.StorePIVCertificateRequest{
		Backend:     req.GetBackend(),
		Slot:        req.GetSlot(),
		Certificate: req.GetCertificate(),
		Format:      req.GetFormat(),
	}

	if err := xkmssdk.ServiceImportPIVCertificate(ctx, tReq); err != nil {
		return nil, xkmsMapPIVError(err, "import piv certificate")
	}

	return &emptypb.Empty{}, nil
}

// ExportPIVCertificate exports a certificate from a PIV slot.
func (s *XKMSServer) ExportPIVCertificate(ctx context.Context, req *xkmssdk.ProtoGetPIVCertificateRequest) (*xkmssdk.ProtoGetPIVCertificateResponse, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}
	if req.GetFormat() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVFormatRequired.Error())
	}

	tReq := &transportTypes.GetPIVCertificateRequest{
		Backend: req.GetBackend(),
		Slot:    req.GetSlot(),
		Format:  req.GetFormat(),
	}

	resp, err := xkmssdk.ServiceExportPIVCertificate(ctx, tReq)
	if err != nil {
		return nil, xkmsMapPIVError(err, "export piv certificate")
	}

	return &xkmssdk.ProtoGetPIVCertificateResponse{
		Slot:        resp.Slot,
		Certificate: resp.Certificate,
		Format:      resp.Format,
	}, nil
}

// GeneratePIVCSR generates a certificate signing request for a PIV slot key.
func (s *XKMSServer) GeneratePIVCSR(ctx context.Context, req *xkmssdk.ProtoGeneratePIVCSRRequest) (*xkmssdk.ProtoGeneratePIVCSRResponse, error) {
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVBackendRequired.Error())
	}
	if req.GetSlot() == "" {
		return nil, status.Error(codes.InvalidArgument, ErrXKMSPIVSlotRequired.Error())
	}

	tReq := &transportTypes.GeneratePIVCSRRequest{
		Backend: req.GetBackend(),
		Slot:    req.GetSlot(),
		Subject: req.GetSubject(),
	}

	resp, err := xkmssdk.ServiceGeneratePIVCSR(ctx, tReq)
	if err != nil {
		return nil, xkmsMapPIVError(err, "generate piv csr")
	}

	return &xkmssdk.ProtoGeneratePIVCSRResponse{
		Slot: resp.Slot,
		Csr:  resp.CSR,
	}, nil
}
