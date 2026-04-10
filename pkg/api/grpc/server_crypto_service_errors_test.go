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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newTestCryptoServerWithError(errCode dberrors.ErrorCode) *CryptoServer {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svcErr := &dberrors.DragonError{Code: errCode, Op: "test"}
	return NewCryptoServer(&mockCryptoService{
		encryptFunc: func(_ context.Context, _ *service.EncryptRequest) (*service.EncryptResponse, error) {
			return nil, svcErr
		},
		decryptFunc: func(_ context.Context, _ *service.DecryptRequest) (*service.DecryptResponse, error) {
			return nil, svcErr
		},
		rewrapFunc: func(_ context.Context, _ *service.RewrapRequest) (*service.RewrapResponse, error) {
			return nil, svcErr
		},
		encryptBatchFunc: func(_ context.Context, _ *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
			return nil, svcErr
		},
		decryptBatchFunc: func(_ context.Context, _ *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
			return nil, svcErr
		},
		generateDataKeyFunc: func(_ context.Context, _ *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
			return nil, svcErr
		},
		generateWrappedDataKeyFunc: func(_ context.Context, _ *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
			return nil, svcErr
		},
		signFunc: func(_ context.Context, _ *service.SignRequest) (*service.SignResponse, error) {
			return nil, svcErr
		},
		verifyFunc: func(_ context.Context, _ *service.VerifyRequest) (*service.VerifyResponse, error) {
			return nil, svcErr
		},
		generateHMACFunc: func(_ context.Context, _ *service.HMACRequest) (*service.HMACResponse, error) {
			return nil, svcErr
		},
		verifyHMACFunc: func(_ context.Context, _ *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
			return nil, svcErr
		},
	}, log)
}

func TestCryptoServer_Encrypt_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrNotFound)
	_, err := srv.Encrypt(context.Background(), &pb.CryptoEncryptRequest{
		KeyId: "k1", Plaintext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestCryptoServer_Decrypt_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrInternal)
	_, err := srv.Decrypt(context.Background(), &pb.CryptoDecryptRequest{
		KeyId: "k1", Ciphertext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCryptoServer_Rewrap_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrNotFound)
	_, err := srv.Rewrap(context.Background(), &pb.RewrapRequest{
		KeyId: "k1", Ciphertext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestCryptoServer_EncryptBatch_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrInternal)
	_, err := srv.EncryptBatch(context.Background(), &pb.EncryptBatchRequest{
		KeyId: "k1", Items: []*pb.BatchEncryptItem{{Plaintext: []byte("data")}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCryptoServer_DecryptBatch_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrInternal)
	_, err := srv.DecryptBatch(context.Background(), &pb.DecryptBatchRequest{
		KeyId: "k1", Items: []*pb.BatchDecryptItem{{Ciphertext: []byte("data")}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCryptoServer_GenerateDataKey_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrNotFound)
	_, err := srv.GenerateDataKey(context.Background(), &pb.GenerateDataKeyRequest{
		KeyId: "k1", KeySpec: &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestCryptoServer_GenerateWrappedDataKey_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrNotFound)
	_, err := srv.GenerateWrappedDataKey(context.Background(), &pb.GenerateWrappedDataKeyRequest{
		KeyId: "k1", KeySpec: &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestCryptoServer_Sign_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrInternal)
	_, err := srv.Sign(context.Background(), &pb.SignRequest{
		KeyId: "k1", Message: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCryptoServer_Verify_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrInternal)
	_, err := srv.Verify(context.Background(), &pb.VerifyRequest{
		KeyId: "k1", Message: []byte("data"), Signature: []byte("sig"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCryptoServer_GenerateHMAC_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrNotFound)
	_, err := srv.GenerateHMAC(context.Background(), &pb.HMACRequest{
		KeyId: "k1", Message: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestCryptoServer_VerifyHMAC_ServiceError(t *testing.T) {
	srv := newTestCryptoServerWithError(dberrors.ErrInternal)
	_, err := srv.VerifyHMAC(context.Background(), &pb.VerifyHMACRequest{
		KeyId: "k1", Message: []byte("data"), Hmac: []byte("hmac"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
