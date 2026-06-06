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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errCrypto is a sentinel error returned by the fake crypto service to
// exercise the error-propagation path through the embedded handlers.
var errCrypto = errors.New("crypto: boom")

// fakeCryptoService is a configurable CryptoService used to drive the
// embedded crypto handlers through their happy and error paths. Each method
// returns errCrypto when failNext is true, otherwise a zero-value response.
type fakeCryptoService struct {
	failNext bool
}

func (f *fakeCryptoService) maybeErr() error {
	if f.failNext {
		return errCrypto
	}
	return nil
}

func (f *fakeCryptoService) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.EncryptResponse{}, nil
}

func (f *fakeCryptoService) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.DecryptResponse{}, nil
}

func (f *fakeCryptoService) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.RewrapResponse{}, nil
}

func (f *fakeCryptoService) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.EncryptBatchResponse{}, nil
}

func (f *fakeCryptoService) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.DecryptBatchResponse{}, nil
}

func (f *fakeCryptoService) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GenerateDataKeyResponse{}, nil
}

func (f *fakeCryptoService) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GenerateWrappedDataKeyResponse{}, nil
}

func (f *fakeCryptoService) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.SignResponse{}, nil
}

func (f *fakeCryptoService) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.VerifyResponse{}, nil
}

func (f *fakeCryptoService) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.HMACResponse{}, nil
}

func (f *fakeCryptoService) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.VerifyHMACResponse{}, nil
}

func (f *fakeCryptoService) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.GenerateRandomResponse{}, nil
}

func (f *fakeCryptoService) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	if err := f.maybeErr(); err != nil {
		return nil, err
	}
	return &service.HashResponse{}, nil
}

// cryptoMock embeds the base mock and overrides Crypto() so the embedded
// crypto handlers can resolve a non-nil sub-service.
type cryptoMock struct {
	*mockQuicBaaS
	crypto service.CryptoService
}

func (m *cryptoMock) Crypto() service.CryptoService { return m.crypto }

func newCryptoTransport(t *testing.T, crypto service.CryptoService) *Transport {
	t.Helper()
	tr, err := New(&cryptoMock{mockQuicBaaS: &mockQuicBaaS{}, crypto: crypto})
	require.NoError(t, err)
	return tr
}

// cryptoCase describes a single crypto method dispatch: the wire method,
// a valid service request value, and a wrong-typed request value.
type cryptoCase struct {
	method   string
	validReq interface{}
	wrongReq interface{}
}

func cryptoCases() []cryptoCase {
	return []cryptoCase{
		{"Encrypt", &service.EncryptRequest{}, &service.DecryptRequest{}},
		{"Decrypt", &service.DecryptRequest{}, &service.EncryptRequest{}},
		{"Rewrap", &service.RewrapRequest{}, &service.EncryptRequest{}},
		{"EncryptBatch", &service.EncryptBatchRequest{}, &service.EncryptRequest{}},
		{"DecryptBatch", &service.DecryptBatchRequest{}, &service.EncryptRequest{}},
		{"GenerateDataKey", &service.GenerateDataKeyRequest{}, &service.EncryptRequest{}},
		{"GenerateWrappedDataKey", &service.GenerateWrappedDataKeyRequest{}, &service.EncryptRequest{}},
		{"Sign", &service.SignRequest{}, &service.EncryptRequest{}},
		{"Verify", &service.VerifyRequest{}, &service.EncryptRequest{}},
		{"GenerateHMAC", &service.HMACRequest{}, &service.EncryptRequest{}},
		{"VerifyHMAC", &service.VerifyHMACRequest{}, &service.EncryptRequest{}},
		{"GenerateRandom", &service.GenerateRandomRequest{}, &service.EncryptRequest{}},
		{"Hash", &service.HashRequest{}, &service.EncryptRequest{}},
	}
}

func TestTransport_CryptoOperations_Success(t *testing.T) {
	tr := newCryptoTransport(t, &fakeCryptoService{})
	ctx := context.Background()

	for _, tc := range cryptoCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Crypto", tc.method, tc.validReq)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func TestTransport_CryptoOperations_ServiceError(t *testing.T) {
	tr := newCryptoTransport(t, &fakeCryptoService{failNext: true})
	ctx := context.Background()

	for _, tc := range cryptoCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Crypto", tc.method, tc.validReq)
			require.ErrorIs(t, err, errCrypto)
			assert.Nil(t, resp)
		})
	}
}

func TestTransport_CryptoOperations_WrongType(t *testing.T) {
	tr := newCryptoTransport(t, &fakeCryptoService{})
	ctx := context.Background()

	for _, tc := range cryptoCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Crypto", tc.method, tc.wrongReq)
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

func TestTransport_CryptoOperations_NotConfigured(t *testing.T) {
	// Base mock returns a nil Crypto() sub-service.
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range cryptoCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.Crypto", tc.method, tc.validReq)
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}
