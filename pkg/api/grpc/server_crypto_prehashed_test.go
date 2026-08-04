package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
