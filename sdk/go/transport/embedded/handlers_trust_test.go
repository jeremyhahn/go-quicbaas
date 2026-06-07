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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
)

// errTrust is a sentinel returned by the fake trust service to drive
// error-propagation paths through the embedded handlers.
var errTrust = errors.New("trust: boom")

// fakeTrustService is a configurable TrustService. When failNext is true
// every method returns errTrust; otherwise it returns a populated
// response.
type fakeTrustService struct {
	failNext bool
}

func (f *fakeTrustService) CrossSignedRootPool(ctx context.Context, _ *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
	if f.failNext {
		return nil, errTrust
	}
	return &service.CrossSignedRootPoolResponse{
		RootPEMs: [][]byte{[]byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")},
	}, nil
}

func (f *fakeTrustService) AutoImportAIA(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
	if f.failNext {
		return nil, errTrust
	}
	return &service.AutoImportAIAResponse{Imported: len(req.ChainDER)}, nil
}

func (f *fakeTrustService) PublishDANE(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
	if f.failNext {
		return nil, errTrust
	}
	return &service.PublishDANEResponse{Published: 4}, nil
}

// trustMock embeds the base mock and overrides Trust().
type trustMock struct {
	*mockQuicBaaS
	trust service.TrustService
}

func (m *trustMock) Trust() service.TrustService { return m.trust }

func newTrustTransport(t *testing.T, trust service.TrustService) *Transport {
	t.Helper()
	tr, err := New(&trustMock{mockQuicBaaS: &mockQuicBaaS{}, trust: trust})
	require.NoError(t, err)
	return tr
}

func TestTransport_TrustCrossSignedRootPool(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{})
	resp, err := tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool",
		&trustclient.CrossSignedRootPoolRequest{})
	require.NoError(t, err)
	typed, ok := resp.(*trustclient.CrossSignedRootPoolResponse)
	require.True(t, ok)
	require.Len(t, typed.RootPEMs, 1)
}

func TestTransport_TrustCrossSignedRootPool_NilRequest(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{})
	resp, err := tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool", nil)
	require.NoError(t, err)
	_, ok := resp.(*trustclient.CrossSignedRootPoolResponse)
	require.True(t, ok)
}

func TestTransport_TrustAutoImportAIA(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{})
	resp, err := tr.Request(context.Background(), "quicbaas.Trust", "AutoImportAIA",
		&trustclient.AutoImportAIARequest{ChainDER: [][]byte{{0x30, 0x82}, {0x30, 0x82}}})
	require.NoError(t, err)
	typed, ok := resp.(*trustclient.AutoImportAIAResponse)
	require.True(t, ok)
	assert.Equal(t, 2, typed.Imported)
}

func TestTransport_TrustOperations_ServiceError(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{failNext: true})

	_, err := tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool",
		&trustclient.CrossSignedRootPoolRequest{})
	require.ErrorIs(t, err, errTrust)

	_, err = tr.Request(context.Background(), "quicbaas.Trust", "AutoImportAIA",
		&trustclient.AutoImportAIARequest{ChainDER: [][]byte{{0x30}}})
	require.ErrorIs(t, err, errTrust)
}

func TestTransport_TrustOperations_WrongType(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{})

	_, err := tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool", &service.PutRequest{})
	require.Error(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Trust", "AutoImportAIA", &service.PutRequest{})
	require.Error(t, err)
}

func TestTransport_TrustOperations_NotConfigured(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Trust", "CrossSignedRootPool",
		&trustclient.CrossSignedRootPoolRequest{})
	require.Error(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Trust", "AutoImportAIA",
		&trustclient.AutoImportAIARequest{ChainDER: [][]byte{{0x30}}})
	require.Error(t, err)
}

func TestTransport_TrustPublishDANE(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{})
	resp, err := tr.Request(context.Background(), "quicbaas.Trust", "PublishDANE",
		&trustclient.PublishDANERequest{Name: "kms.example.com", Port: 443, CertDER: []byte{0x30, 0x82}})
	require.NoError(t, err)
	typed, ok := resp.(*trustclient.PublishDANEResponse)
	require.True(t, ok)
	assert.Equal(t, 4, typed.Published)
}

func TestTransport_TrustPublishDANE_WrongType(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{})
	_, err := tr.Request(context.Background(), "quicbaas.Trust", "PublishDANE", &service.PutRequest{})
	require.Error(t, err)
}

func TestTransport_TrustPublishDANE_ServiceError(t *testing.T) {
	tr := newTrustTransport(t, &fakeTrustService{failNext: true})
	_, err := tr.Request(context.Background(), "quicbaas.Trust", "PublishDANE",
		&trustclient.PublishDANERequest{Name: "kms.example.com", Port: 443, CertDER: []byte{0x30}})
	require.ErrorIs(t, err, errTrust)
}
