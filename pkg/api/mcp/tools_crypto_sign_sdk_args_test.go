//go:build mcp

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// sdkToolArgs builds tool arguments the way the SDK's MCP transport does for
// a request without a dedicated handler: the service request's JSON form.
func sdkToolArgs(t *testing.T, req interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	var args map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &args))
	return args
}

// The quicbaas_crypto_sign and quicbaas_crypto_verify tools must hand the
// crypto service the same request gRPC does for the SDK's arguments (#1700).
func TestCryptoSignVerify_SDKArgsReachServiceLikeGRPC(t *testing.T) {
	msg := []byte("hello signer")
	var gotSign *service.SignRequest
	var gotVerify *service.VerifyRequest
	mock := &mocks.MockCryptoService{
		SignFunc: func(_ context.Context, req *service.SignRequest) (*service.SignResponse, error) {
			gotSign = req
			return &service.SignResponse{KeyID: req.KeyID, Signature: []byte("sig"), HashAlgorithm: req.HashAlgorithm}, nil
		},
		VerifyFunc: func(_ context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
			gotVerify = req
			return &service.VerifyResponse{Valid: true}, nil
		},
	}
	registry := NewToolRegistry(cryptoAccessorService{QuicBaaS: &mockService{}, crypto: mock}, newTestLogger(t))

	_, err := registry.executeCryptoSign(context.Background(), sdkToolArgs(t, &service.SignRequest{
		KeyID: "sign-key", Message: msg, HashAlgorithm: service.HashAlgSHA256,
	}))
	require.NoError(t, err)
	require.NotNil(t, gotSign)
	assert.Equal(t, "sign-key", gotSign.KeyID)
	assert.Equal(t, msg, gotSign.Message)
	assert.Equal(t, service.HashAlgSHA256, gotSign.HashAlgorithm)

	_, err = registry.executeCryptoVerify(context.Background(), sdkToolArgs(t, &service.VerifyRequest{
		KeyID: "sign-key", Message: msg, Signature: []byte("sig"), HashAlgorithm: service.HashAlgSHA256,
	}))
	require.NoError(t, err)
	require.NotNil(t, gotVerify)
	assert.Equal(t, msg, gotVerify.Message)
	assert.Equal(t, []byte("sig"), gotVerify.Signature)
	assert.Equal(t, service.HashAlgSHA256, gotVerify.HashAlgorithm)
}
