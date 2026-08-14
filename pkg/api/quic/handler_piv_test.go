//go:build quic

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

package quic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errQUICPIV drives error propagation through the PIV handlers.
var errQUICPIV = errors.New("piv: boom")

// fakePIVService records its input and replays a fixed response.
type fakePIVService struct {
	failNext bool

	lastList     *xkmstransport.ListPIVSlotsRequest
	lastGet      *xkmstransport.GetPIVCertificateRequest
	lastStore    *xkmstransport.StorePIVCertificateRequest
	lastDelete   *xkmstransport.DeletePIVCertificateRequest
	lastGenerate *xkmstransport.GeneratePIVKeyRequest
	lastImport   *xkmstransport.StorePIVCertificateRequest
	lastExport   *xkmstransport.GetPIVCertificateRequest
	lastCSR      *xkmstransport.GeneratePIVCSRRequest
}

func (f *fakePIVService) ListPIVSlots(_ context.Context, req *xkmstransport.ListPIVSlotsRequest) (*xkmstransport.ListPIVSlotsResponse, error) {
	f.lastList = req
	if f.failNext {
		return nil, errQUICPIV
	}
	return &xkmstransport.ListPIVSlotsResponse{
		Slots: []xkmstransport.PIVSlotStatus{{Slot: "9a", HasCert: true}},
	}, nil
}

func (f *fakePIVService) GetPIVCertificate(_ context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	f.lastGet = req
	if f.failNext {
		return nil, errQUICPIV
	}
	return &xkmstransport.GetPIVCertificateResponse{Slot: req.Slot, Certificate: []byte("cert"), Format: req.Format}, nil
}

func (f *fakePIVService) StorePIVCertificate(_ context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	f.lastStore = req
	if f.failNext {
		return errQUICPIV
	}
	return nil
}

func (f *fakePIVService) DeletePIVCertificate(_ context.Context, req *xkmstransport.DeletePIVCertificateRequest) error {
	f.lastDelete = req
	if f.failNext {
		return errQUICPIV
	}
	return nil
}

func (f *fakePIVService) GeneratePIVKey(_ context.Context, req *xkmstransport.GeneratePIVKeyRequest) (*xkmstransport.GeneratePIVKeyResponse, error) {
	f.lastGenerate = req
	if f.failNext {
		return nil, errQUICPIV
	}
	return &xkmstransport.GeneratePIVKeyResponse{Slot: req.Slot, Certificate: []byte("cert"), PublicKey: []byte("pub")}, nil
}

func (f *fakePIVService) ImportPIVCertificate(_ context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	f.lastImport = req
	if f.failNext {
		return errQUICPIV
	}
	return nil
}

func (f *fakePIVService) ExportPIVCertificate(_ context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	f.lastExport = req
	if f.failNext {
		return nil, errQUICPIV
	}
	return &xkmstransport.GetPIVCertificateResponse{Slot: req.Slot, Certificate: []byte("cert"), Format: req.Format}, nil
}

func (f *fakePIVService) GeneratePIVCSR(_ context.Context, req *xkmstransport.GeneratePIVCSRRequest) (*xkmstransport.GeneratePIVCSRResponse, error) {
	f.lastCSR = req
	if f.failNext {
		return nil, errQUICPIV
	}
	return &xkmstransport.GeneratePIVCSRResponse{Slot: req.Slot, CSR: []byte("csr")}, nil
}

// newPIVTestHandler builds a QUIC Handler whose PIV() returns the supplied
// service.
func newPIVTestHandler(t *testing.T, svc service.PIVService) *Handler {
	t.Helper()
	return NewHandler(&mockService{pivSvc: svc}, nil, nil, nil, createCertificateTestLogger())
}

// pivRequest builds a wire request carrying the sub-op byte followed by the
// JSON body, exactly as the SDK QUIC transport encodes it.
func pivRequest(t *testing.T, subOp byte, body interface{}) *Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	full := append([]byte{subOp}, payload...)
	return &Request{Opcode: OpOrchestrator, Payload: full}
}

func TestHandler_PIVDispatch_ListSlots(t *testing.T) {
	fake := &fakePIVService{}
	h := newPIVTestHandler(t, fake)

	resp := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVListSlots, &xkmstransport.ListPIVSlotsRequest{Backend: "pkcs11"}))
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	var decoded xkmstransport.ListPIVSlotsResponse
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Slots) != 1 || decoded.Slots[0].Slot != "9a" {
		t.Fatalf("unexpected slots: %+v", decoded.Slots)
	}
	if fake.lastList == nil || fake.lastList.Backend != "pkcs11" {
		t.Fatalf("request did not reach the service: %+v", fake.lastList)
	}
}

func TestHandler_PIVDispatch_GetCert(t *testing.T) {
	fake := &fakePIVService{}
	h := newPIVTestHandler(t, fake)

	resp := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVGetCert, &xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"}))
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	var decoded xkmstransport.GetPIVCertificateResponse
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Slot != "9a" || string(decoded.Certificate) != "cert" {
		t.Fatalf("unexpected response: %+v", decoded)
	}
	if fake.lastGet == nil || fake.lastGet.Backend != "file" {
		t.Fatalf("request did not reach the service: %+v", fake.lastGet)
	}
}

func TestHandler_PIVDispatch_VoidVerbs(t *testing.T) {
	fake := &fakePIVService{}
	h := newPIVTestHandler(t, fake)

	store := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVStoreCert, &xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9a", Certificate: []byte("c"), Format: "pem"}))
	if store.Status != StatusOK || len(store.Payload) != 0 {
		t.Fatalf("store: status = %v payload = %q", store.Status, store.Payload)
	}
	if fake.lastStore == nil || fake.lastStore.Slot != "9a" {
		t.Fatalf("store request did not reach the service: %+v", fake.lastStore)
	}

	del := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVDeleteCert, &xkmstransport.DeletePIVCertificateRequest{Backend: "file", Slot: "9a"}))
	if del.Status != StatusOK || len(del.Payload) != 0 {
		t.Fatalf("delete: status = %v payload = %q", del.Status, del.Payload)
	}
	if fake.lastDelete == nil || fake.lastDelete.Slot != "9a" {
		t.Fatalf("delete request did not reach the service: %+v", fake.lastDelete)
	}

	imp := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVImportCert, &xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9c", Certificate: []byte("c"), Format: "der"}))
	if imp.Status != StatusOK || len(imp.Payload) != 0 {
		t.Fatalf("import: status = %v payload = %q", imp.Status, imp.Payload)
	}
	if fake.lastImport == nil || fake.lastImport.Slot != "9c" {
		t.Fatalf("import request did not reach the service: %+v", fake.lastImport)
	}
}

func TestHandler_PIVDispatch_GenerateKeyExportCSR(t *testing.T) {
	fake := &fakePIVService{}
	h := newPIVTestHandler(t, fake)

	gen := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVGenerateKey, &xkmstransport.GeneratePIVKeyRequest{Backend: "tpm2", Slot: "9a", Algorithm: "ecdsap256"}))
	if gen.Status != StatusOK {
		t.Fatalf("generateKey: status = %v payload = %s", gen.Status, gen.Payload)
	}
	var genResp xkmstransport.GeneratePIVKeyResponse
	if err := json.Unmarshal(gen.Payload, &genResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(genResp.PublicKey) != "pub" {
		t.Fatalf("unexpected generateKey response: %+v", genResp)
	}
	if fake.lastGenerate == nil || fake.lastGenerate.Algorithm != "ecdsap256" {
		t.Fatalf("generate request did not reach the service: %+v", fake.lastGenerate)
	}

	exp := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVExportCert, &xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9d", Format: "der"}))
	if exp.Status != StatusOK {
		t.Fatalf("export: status = %v payload = %s", exp.Status, exp.Payload)
	}
	if fake.lastExport == nil || fake.lastExport.Slot != "9d" {
		t.Fatalf("export request did not reach the service: %+v", fake.lastExport)
	}

	csr := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVGenerateCSR, &xkmstransport.GeneratePIVCSRRequest{Backend: "file", Slot: "9a", Subject: "CN=test"}))
	if csr.Status != StatusOK {
		t.Fatalf("csr: status = %v payload = %s", csr.Status, csr.Payload)
	}
	var csrResp xkmstransport.GeneratePIVCSRResponse
	if err := json.Unmarshal(csr.Payload, &csrResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(csrResp.CSR) != "csr" {
		t.Fatalf("unexpected csr response: %+v", csrResp)
	}
	if fake.lastCSR == nil || fake.lastCSR.Subject != "CN=test" {
		t.Fatalf("csr request did not reach the service: %+v", fake.lastCSR)
	}
}

func TestHandler_PIVDispatch_ServiceErrorPropagates(t *testing.T) {
	fake := &fakePIVService{failNext: true}
	h := newPIVTestHandler(t, fake)

	resp := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVListSlots, &xkmstransport.ListPIVSlotsRequest{Backend: "pkcs11"}))
	if resp.Status == StatusOK {
		t.Fatalf("expected error status, got OK")
	}
}

func TestHandler_PIVDispatch_NilServiceUnavailable(t *testing.T) {
	h := newPIVTestHandler(t, nil)

	resp := h.handleOrchestratorDispatch(context.Background(),
		pivRequest(t, SubOpPIVListSlots, &xkmstransport.ListPIVSlotsRequest{Backend: "pkcs11"}))
	if resp.Status == StatusOK {
		t.Fatalf("expected error status for nil PIV service, got OK")
	}
}

func TestHandler_PIVDispatch_InvalidBody(t *testing.T) {
	fake := &fakePIVService{}
	h := newPIVTestHandler(t, fake)

	req := &Request{Opcode: OpOrchestrator, Payload: append([]byte{SubOpPIVListSlots}, []byte("{not json")...)}
	resp := h.handleOrchestratorDispatch(context.Background(), req)
	if resp.Status == StatusOK {
		t.Fatalf("expected error status for malformed body, got OK")
	}
}

// TestPIVSubOpPermissions_Complete pins every PIV sub-op to a piv:* qrdb
// permission so a new verb cannot ship without an authorization decision.
func TestPIVSubOpPermissions_Complete(t *testing.T) {
	for subOp := SubOpPIVListSlots; subOp <= SubOpPIVGenerateCSR; subOp++ {
		perm, ok := pivSubOpPermissions[subOp]
		if !ok {
			t.Fatalf("sub-op 0x%02X has no permission mapping", subOp)
		}
		if perm.resource != "piv" {
			t.Fatalf("sub-op 0x%02X resolves resource %q, want piv", subOp, perm.resource)
		}
	}

	res, act, ok := getQRDBPermission(OpOrchestrator, []byte{SubOpPIVDeleteCert})
	if !ok || res != "piv" || act != "delete" {
		t.Fatalf("getQRDBPermission(piv delete) = %q:%q ok=%v", res, act, ok)
	}
}
