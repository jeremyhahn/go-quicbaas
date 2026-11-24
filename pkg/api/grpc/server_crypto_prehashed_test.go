package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// newPreHashedCaptureServer returns a CryptoServer whose Sign captures the
// service-layer request so tests can assert proto-to-service field mapping.
func newPreHashedCaptureServer(captured **service.SignRequest) *CryptoServer {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	return NewCryptoServer(&mockCryptoService{
		signFunc: func(_ context.Context, req *service.SignRequest) (*service.SignResponse, error) {
			*captured = req
			return &service.SignResponse{
				KeyID:     req.KeyID,
				Signature: []byte("sig"),
				Timestamp: time.Now(),
			}, nil
		},
	}, log)
}

// TestCryptoServer_Sign_PreHashedPassthrough verifies the pre_hashed wire flag
// reaches the service layer so a digest is signed directly without a second
// hash pass.
func TestCryptoServer_Sign_PreHashedPassthrough(t *testing.T) {
	var captured *service.SignRequest
	srv := newPreHashedCaptureServer(&captured)

	_, err := srv.Sign(context.Background(), &pb.SignRequest{
		KeyId:     "k1",
		Message:   []byte{0xde, 0xad, 0xbe, 0xef},
		PreHashed: true,
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.True(t, captured.PreHashed, "pre_hashed must survive the proto-to-service conversion")
}

func TestCryptoServer_Sign_PSSPassthrough(t *testing.T) {
	t.Run("PSS fields reach the service", func(t *testing.T) {
		var captured *service.SignRequest
		srv := newPreHashedCaptureServer(&captured)

		_, err := srv.Sign(context.Background(), &pb.SignRequest{
			KeyId:         "k1",
			Message:       []byte{0xde, 0xad, 0xbe, 0xef},
			HashAlgorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
			Padding:       pb.SignaturePadding_SIGNATURE_PADDING_PSS,
			PssSaltLength: 32,
			Mgf1Hash:      pb.HashAlgorithm_HASH_ALG_SHA256,
		})
		require.NoError(t, err)
		require.NotNil(t, captured)
		assert.Equal(t, service.SignaturePaddingPSS, captured.Padding)
		assert.Equal(t, 32, captured.PSSSaltLength)
		assert.Equal(t, service.HashAlgSHA256, captured.MGF1Hash)
	})

	t.Run("UNSPECIFIED gives empty padding", func(t *testing.T) {
		var captured *service.SignRequest
		srv := newPreHashedCaptureServer(&captured)

		_, err := srv.Sign(context.Background(), &pb.SignRequest{
			KeyId:   "k1",
			Message: []byte{0xde, 0xad, 0xbe, 0xef},
		})
		require.NoError(t, err)
		require.NotNil(t, captured)
		assert.Empty(t, captured.Padding)
		assert.Zero(t, captured.PSSSaltLength)
		assert.Empty(t, captured.MGF1Hash)
	})
}

func TestCryptoServer_Sign_RejectsUnknownMGF1Hash(t *testing.T) {
	t.Run("unknown enum is refused before the service", func(t *testing.T) {
		var captured *service.SignRequest
		srv := newPreHashedCaptureServer(&captured)

		_, err := srv.Sign(context.Background(), &pb.SignRequest{
			KeyId:         "k1",
			Message:       []byte{0xde, 0xad, 0xbe, 0xef},
			HashAlgorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
			Padding:       pb.SignaturePadding_SIGNATURE_PADDING_PSS,
			Mgf1Hash:      pb.HashAlgorithm(999),
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "mgf1_hash")
		assert.Nil(t, captured)
	})

	named := []pb.HashAlgorithm{
		pb.HashAlgorithm_HASH_ALG_SHA256,
		pb.HashAlgorithm_HASH_ALG_SHA384,
		pb.HashAlgorithm_HASH_ALG_SHA512,
		pb.HashAlgorithm_HASH_ALG_SHA3_256,
		pb.HashAlgorithm_HASH_ALG_SHA3_384,
		pb.HashAlgorithm_HASH_ALG_SHA3_512,
		pb.HashAlgorithm_HASH_ALG_BLAKE2B_256,
		pb.HashAlgorithm_HASH_ALG_BLAKE2B_512,
	}
	for _, alg := range named {
		t.Run("named "+alg.String()+" reaches the service", func(t *testing.T) {
			var captured *service.SignRequest
			srv := newPreHashedCaptureServer(&captured)

			_, err := srv.Sign(context.Background(), &pb.SignRequest{
				KeyId:         "k1",
				Message:       []byte{0xde, 0xad, 0xbe, 0xef},
				HashAlgorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
				Padding:       pb.SignaturePadding_SIGNATURE_PADDING_PSS,
				Mgf1Hash:      alg,
			})
			require.NoError(t, err)
			require.NotNil(t, captured)
			assert.Equal(t, convertProtoHashAlgorithm(alg), captured.MGF1Hash)
		})
	}
}

// TestCryptoServer_Sign_NotPreHashedByDefault verifies the default remains
// hash-then-sign when pre_hashed is absent from the request.
func TestCryptoServer_Sign_NotPreHashedByDefault(t *testing.T) {
	var captured *service.SignRequest
	srv := newPreHashedCaptureServer(&captured)

	_, err := srv.Sign(context.Background(), &pb.SignRequest{
		KeyId:   "k1",
		Message: []byte("raw message"),
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.False(t, captured.PreHashed, "raw messages must keep hash-then-sign semantics")
}
