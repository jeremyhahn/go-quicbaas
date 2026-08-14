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

	"google.golang.org/grpc"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// pivServicePrefix is the gRPC-style service identifier the SDK PIV surface
// routes through. It matches the const used by the REST / QUIC / MCP /
// embedded transports.
const pivServicePrefix = "quicbaas.PIV"

// isPIVService reports whether svc names the QuicBaaS PIV service. The SDK
// PIV surface exchanges the go-xkms transport wire structs, which do not
// implement proto.Message, so they must be converted to the generated
// PIVService proto types first.
func isPIVService(svc string) bool {
	return strings.HasPrefix(svc, pivServicePrefix) || svc == "PIV"
}

// pivHandler converts between the go-xkms transport wire structs and the
// generated PIVService proto types, then calls the gRPC PIVService
// registered by the server lifecycle.
type pivHandler struct {
	conn *grpc.ClientConn
}

// newPIVHandler creates a new PIV handler bound to conn.
func newPIVHandler(conn *grpc.ClientConn) *pivHandler {
	return &pivHandler{conn: conn}
}

// pivMethodHandler is the per-method conversion function type used for O(1)
// dispatch.
type pivMethodHandler func(*pivHandler, context.Context, pb.PIVServiceClient, interface{}) (interface{}, error)

// pivMethods maps SDK method names onto their conversion handlers.
var pivMethods = map[string]pivMethodHandler{
	"ListPIVSlots":         (*pivHandler).handleListPIVSlots,
	"GetPIVCertificate":    (*pivHandler).handleGetPIVCertificate,
	"ExportPIVCertificate": (*pivHandler).handleExportPIVCertificate,
	"StorePIVCertificate":  (*pivHandler).handleStorePIVCertificate,
	"ImportPIVCertificate": (*pivHandler).handleImportPIVCertificate,
	"DeletePIVCertificate": (*pivHandler).handleDeletePIVCertificate,
	"GeneratePIVKey":       (*pivHandler).handleGeneratePIVKey,
	"GeneratePIVCSR":       (*pivHandler).handleGeneratePIVCSR,
}

// handleRequest routes PIV requests to the appropriate handler based on
// method name.
func (h *pivHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	fn, ok := pivMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("pivHandler.handleRequest",
			"method not exposed by PIVService gRPC proto: "+method)
	}
	return fn(h, ctx, pb.NewPIVServiceClient(h.conn), req)
}

func (h *pivHandler) handleListPIVSlots(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.ListPIVSlotsRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("pivHandler.handleListPIVSlots",
			"invalid request type, expected *xkmstransport.ListPIVSlotsRequest")
	}

	pbResp, err := client.ListPIVSlots(ctx, &pb.PIVListSlotsRequest{
		Backend:  sdkReq.Backend,
		TenantId: sdkReq.TenantID,
		Page: &pb.PIVPageRequest{
			Page:      int32(sdkReq.Page),
			PageSize:  int32(sdkReq.PageSize),
			SortField: sdkReq.SortField,
			SortOrder: int32(sdkReq.SortOrder),
		},
	})
	if err != nil {
		return nil, convertGRPCError(err, "pivHandler.handleListPIVSlots")
	}

	resp := &xkmstransport.ListPIVSlotsResponse{
		Slots: make([]xkmstransport.PIVSlotStatus, 0, len(pbResp.Slots)),
	}
	if pbResp.Page != nil {
		resp.PageResponse = xkmstransport.PageResponse{
			Page:     int(pbResp.Page.Page),
			PageSize: int(pbResp.Page.PageSize),
			Total:    int(pbResp.Page.Total),
			HasMore:  pbResp.Page.HasMore,
		}
	}
	for _, slot := range pbResp.Slots {
		if slot == nil {
			continue
		}
		resp.Slots = append(resp.Slots, xkmstransport.PIVSlotStatus{
			Slot:        slot.Slot,
			Name:        slot.Name,
			Description: slot.Description,
			HasCert:     slot.HasCert,
			HasKey:      slot.HasKey,
			Subject:     slot.Subject,
			Issuer:      slot.Issuer,
			Algorithm:   slot.Algorithm,
			KeySize:     int(slot.KeySize),
			NotAfter:    slot.NotAfter,
			Fingerprint: slot.Fingerprint,
			Backend:     slot.Backend,
		})
	}
	return resp, nil
}

// pivGetCertificate is the shared body of GetPIVCertificate and
// ExportPIVCertificate: the verbs exchange identical wire shapes and differ
// only in which RPC they invoke.
func (h *pivHandler) pivGetCertificate(ctx context.Context, req interface{}, op string,
	call func(context.Context, *pb.PIVGetCertificateRequest, ...grpc.CallOption) (*pb.PIVGetCertificateResponse, error)) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.GetPIVCertificateRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput(op,
			"invalid request type, expected *xkmstransport.GetPIVCertificateRequest")
	}

	pbResp, err := call(ctx, &pb.PIVGetCertificateRequest{
		Backend:  sdkReq.Backend,
		Slot:     sdkReq.Slot,
		Format:   sdkReq.Format,
		TenantId: sdkReq.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}

	return &xkmstransport.GetPIVCertificateResponse{
		Slot:        pbResp.Slot,
		Certificate: pbResp.Certificate,
		Format:      pbResp.Format,
	}, nil
}

func (h *pivHandler) handleGetPIVCertificate(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	return h.pivGetCertificate(ctx, req, "pivHandler.handleGetPIVCertificate", client.GetPIVCertificate)
}

func (h *pivHandler) handleExportPIVCertificate(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	return h.pivGetCertificate(ctx, req, "pivHandler.handleExportPIVCertificate", client.ExportPIVCertificate)
}

// pivStoreCertificate is the shared body of StorePIVCertificate and
// ImportPIVCertificate.
func (h *pivHandler) pivStoreCertificate(ctx context.Context, req interface{}, op string,
	call func(context.Context, *pb.PIVStoreCertificateRequest, ...grpc.CallOption) (*pb.PIVStoreCertificateResponse, error)) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.StorePIVCertificateRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput(op,
			"invalid request type, expected *xkmstransport.StorePIVCertificateRequest")
	}

	if _, err := call(ctx, &pb.PIVStoreCertificateRequest{
		Backend:     sdkReq.Backend,
		Slot:        sdkReq.Slot,
		Certificate: sdkReq.Certificate,
		Format:      sdkReq.Format,
		TenantId:    sdkReq.TenantID,
	}); err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

func (h *pivHandler) handleStorePIVCertificate(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	return h.pivStoreCertificate(ctx, req, "pivHandler.handleStorePIVCertificate", client.StorePIVCertificate)
}

func (h *pivHandler) handleImportPIVCertificate(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	return h.pivStoreCertificate(ctx, req, "pivHandler.handleImportPIVCertificate", client.ImportPIVCertificate)
}

func (h *pivHandler) handleDeletePIVCertificate(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.DeletePIVCertificateRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("pivHandler.handleDeletePIVCertificate",
			"invalid request type, expected *xkmstransport.DeletePIVCertificateRequest")
	}

	if _, err := client.DeletePIVCertificate(ctx, &pb.PIVDeleteCertificateRequest{
		Backend:  sdkReq.Backend,
		Slot:     sdkReq.Slot,
		TenantId: sdkReq.TenantID,
	}); err != nil {
		return nil, convertGRPCError(err, "pivHandler.handleDeletePIVCertificate")
	}
	return nil, nil
}

func (h *pivHandler) handleGeneratePIVKey(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.GeneratePIVKeyRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("pivHandler.handleGeneratePIVKey",
			"invalid request type, expected *xkmstransport.GeneratePIVKeyRequest")
	}

	pbResp, err := client.GeneratePIVKey(ctx, &pb.PIVGenerateKeyRequest{
		Backend:      sdkReq.Backend,
		Slot:         sdkReq.Slot,
		Algorithm:    sdkReq.Algorithm,
		Subject:      sdkReq.Subject,
		Organization: sdkReq.Organization,
		Country:      sdkReq.Country,
		OutputFormat: sdkReq.OutputFormat,
		ValidityDays: int32(sdkReq.ValidityDays),
		TenantId:     sdkReq.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "pivHandler.handleGeneratePIVKey")
	}

	return &xkmstransport.GeneratePIVKeyResponse{
		Slot:        pbResp.Slot,
		Certificate: pbResp.Certificate,
		PublicKey:   pbResp.PublicKey,
		CSR:         pbResp.Csr,
		ChainPEM:    pbResp.ChainPem,
	}, nil
}

func (h *pivHandler) handleGeneratePIVCSR(ctx context.Context, client pb.PIVServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.GeneratePIVCSRRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("pivHandler.handleGeneratePIVCSR",
			"invalid request type, expected *xkmstransport.GeneratePIVCSRRequest")
	}

	pbResp, err := client.GeneratePIVCSR(ctx, &pb.PIVGenerateCSRRequest{
		Backend:            sdkReq.Backend,
		Slot:               sdkReq.Slot,
		Subject:            sdkReq.Subject,
		Organization:       sdkReq.Organization,
		OrganizationalUnit: sdkReq.OrganizationalUnit,
		Country:            sdkReq.Country,
		State:              sdkReq.State,
		Locality:           sdkReq.Locality,
		TenantId:           sdkReq.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "pivHandler.handleGeneratePIVCSR")
	}

	return &xkmstransport.GeneratePIVCSRResponse{
		Slot: pbResp.Slot,
		CSR:  pbResp.Csr,
	}, nil
}
