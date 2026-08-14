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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errPIV is a sentinel returned by the fake PIV service to drive
// error-propagation paths through the embedded handlers.
var errPIV = errors.New("piv: boom")

// fakePIVService is a configurable PIVService. When failNext is true every
// method returns errPIV; otherwise it returns a populated response and
// records the request it received.
type fakePIVService struct {
	failNext bool

	lastListReq   *xkmstransport.ListPIVSlotsRequest
	lastGetReq    *xkmstransport.GetPIVCertificateRequest
	lastStoreReq  *xkmstransport.StorePIVCertificateRequest
	lastDeleteReq *xkmstransport.DeletePIVCertificateRequest
	lastGenReq    *xkmstransport.GeneratePIVKeyRequest
	lastImportReq *xkmstransport.StorePIVCertificateRequest
	lastExportReq *xkmstransport.GetPIVCertificateRequest
	lastCSRReq    *xkmstransport.GeneratePIVCSRRequest
}

func (f *fakePIVService) ListPIVSlots(ctx context.Context, req *xkmstransport.ListPIVSlotsRequest) (*xkmstransport.ListPIVSlotsResponse, error) {
	if f.failNext {
		return nil, errPIV
	}
	f.lastListReq = req
	return &xkmstransport.ListPIVSlotsResponse{
		Slots: []xkmstransport.PIVSlotStatus{{Slot: "9a"}},
	}, nil
}

func (f *fakePIVService) GetPIVCertificate(ctx context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	if f.failNext {
		return nil, errPIV
	}
	f.lastGetReq = req
	return &xkmstransport.GetPIVCertificateResponse{Slot: req.Slot}, nil
}

func (f *fakePIVService) StorePIVCertificate(ctx context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	if f.failNext {
		return errPIV
	}
	f.lastStoreReq = req
	return nil
}

func (f *fakePIVService) DeletePIVCertificate(ctx context.Context, req *xkmstransport.DeletePIVCertificateRequest) error {
	if f.failNext {
		return errPIV
	}
	f.lastDeleteReq = req
	return nil
}

func (f *fakePIVService) GeneratePIVKey(ctx context.Context, req *xkmstransport.GeneratePIVKeyRequest) (*xkmstransport.GeneratePIVKeyResponse, error) {
	if f.failNext {
		return nil, errPIV
	}
	f.lastGenReq = req
	return &xkmstransport.GeneratePIVKeyResponse{Slot: req.Slot}, nil
}

func (f *fakePIVService) ImportPIVCertificate(ctx context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	if f.failNext {
		return errPIV
	}
	f.lastImportReq = req
	return nil
}

func (f *fakePIVService) ExportPIVCertificate(ctx context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	if f.failNext {
		return nil, errPIV
	}
	f.lastExportReq = req
	return &xkmstransport.GetPIVCertificateResponse{Slot: req.Slot}, nil
}

func (f *fakePIVService) GeneratePIVCSR(ctx context.Context, req *xkmstransport.GeneratePIVCSRRequest) (*xkmstransport.GeneratePIVCSRResponse, error) {
	if f.failNext {
		return nil, errPIV
	}
	f.lastCSRReq = req
	return &xkmstransport.GeneratePIVCSRResponse{Slot: req.Slot}, nil
}

// pivMock embeds the base mock and overrides PIV().
type pivMock struct {
	*mockQuicBaaS
	piv service.PIVService
}

func (m *pivMock) PIV() service.PIVService { return m.piv }

func newPIVTransport(t *testing.T, piv service.PIVService) *Transport {
	t.Helper()
	tr, err := New(&pivMock{mockQuicBaaS: &mockQuicBaaS{}, piv: piv})
	require.NoError(t, err)
	return tr
}

func TestTransport_PIVListSlots(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{Backend: "file"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.ListPIVSlotsResponse)
	require.True(t, ok)
	require.Len(t, typed.Slots, 1)
	assert.Equal(t, "file", fake.lastListReq.Backend)
}

func TestTransport_PIVGetCertificate(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "GetPIVCertificate",
		&xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.GetPIVCertificateResponse)
	require.True(t, ok)
	assert.Equal(t, "9a", typed.Slot)
}

func TestTransport_PIVStoreCertificate(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "StorePIVCertificate",
		&xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9a"})
	require.NoError(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, "9a", fake.lastStoreReq.Slot)
}

func TestTransport_PIVDeleteCertificate(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "DeletePIVCertificate",
		&xkmstransport.DeletePIVCertificateRequest{Backend: "file", Slot: "9c"})
	require.NoError(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, "9c", fake.lastDeleteReq.Slot)
}

func TestTransport_PIVGenerateKey(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "GeneratePIVKey",
		&xkmstransport.GeneratePIVKeyRequest{Backend: "file", Slot: "9a", Algorithm: "ecdsap256"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.GeneratePIVKeyResponse)
	require.True(t, ok)
	assert.Equal(t, "9a", typed.Slot)
	assert.Equal(t, "ecdsap256", fake.lastGenReq.Algorithm)
}

func TestTransport_PIVImportCertificate(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "ImportPIVCertificate",
		&xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9d"})
	require.NoError(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, "9d", fake.lastImportReq.Slot)
	assert.Nil(t, fake.lastStoreReq, "import must route to ImportPIVCertificate, not StorePIVCertificate")
}

func TestTransport_PIVExportCertificate(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "ExportPIVCertificate",
		&xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9e"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.GetPIVCertificateResponse)
	require.True(t, ok)
	assert.Equal(t, "9e", typed.Slot)
	assert.Nil(t, fake.lastGetReq, "export must route to ExportPIVCertificate, not GetPIVCertificate")
}

func TestTransport_PIVGenerateCSR(t *testing.T) {
	fake := &fakePIVService{}
	tr := newPIVTransport(t, fake)
	resp, err := tr.Request(context.Background(), "quicbaas.PIV", "GeneratePIVCSR",
		&xkmstransport.GeneratePIVCSRRequest{Backend: "file", Slot: "9a", Subject: "CN=test"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.GeneratePIVCSRResponse)
	require.True(t, ok)
	assert.Equal(t, "9a", typed.Slot)
	assert.Equal(t, "CN=test", fake.lastCSRReq.Subject)
}

// TestTransport_PIVOperations_ServiceError verifies every PIV verb
// propagates the service-layer error unchanged.
func TestTransport_PIVOperations_ServiceError(t *testing.T) {
	tr := newPIVTransport(t, &fakePIVService{failNext: true})
	ctx := context.Background()

	requests := map[string]interface{}{
		"ListPIVSlots":         &xkmstransport.ListPIVSlotsRequest{},
		"GetPIVCertificate":    &xkmstransport.GetPIVCertificateRequest{},
		"StorePIVCertificate":  &xkmstransport.StorePIVCertificateRequest{},
		"DeletePIVCertificate": &xkmstransport.DeletePIVCertificateRequest{},
		"GeneratePIVKey":       &xkmstransport.GeneratePIVKeyRequest{},
		"ImportPIVCertificate": &xkmstransport.StorePIVCertificateRequest{},
		"ExportPIVCertificate": &xkmstransport.GetPIVCertificateRequest{},
		"GeneratePIVCSR":       &xkmstransport.GeneratePIVCSRRequest{},
	}
	for method, req := range requests {
		_, err := tr.Request(ctx, "quicbaas.PIV", method, req)
		require.ErrorIs(t, err, errPIV, "method %s", method)
	}
}

// TestTransport_PIVOperations_InvalidRequestType verifies every PIV verb
// rejects a mistyped request with the typed InvalidRequestTypeError.
func TestTransport_PIVOperations_InvalidRequestType(t *testing.T) {
	tr := newPIVTransport(t, &fakePIVService{})
	ctx := context.Background()

	methods := []string{
		"ListPIVSlots", "GetPIVCertificate", "StorePIVCertificate",
		"DeletePIVCertificate", "GeneratePIVKey", "ImportPIVCertificate",
		"ExportPIVCertificate", "GeneratePIVCSR",
	}
	for _, method := range methods {
		_, err := tr.Request(ctx, "quicbaas.PIV", method, struct{ Bogus string }{})
		var typed *InvalidRequestTypeError
		require.ErrorAs(t, err, &typed, "method %s", method)
	}
}

// TestTransport_PIVOperations_ServiceNotConfigured verifies the typed
// not-configured error when PIV() returns nil.
func TestTransport_PIVOperations_ServiceNotConfigured(t *testing.T) {
	tr := newPIVTransport(t, nil)
	_, err := tr.Request(context.Background(), "quicbaas.PIV", "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{})
	var typed *ServiceNotConfiguredError
	require.ErrorAs(t, err, &typed)
}
