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
	"strings"

	"google.golang.org/grpc"

	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// cryptoServicePrefix is the gRPC-style service identifier the SDK crypto
// client routes through. The server exposes it as the Crypto proto service.
const cryptoServicePrefix = "quicbaas.Crypto"

// isCryptoService reports whether svc targets the quicbaas.Crypto surface. The
// crypto SDK client already converts its inputs to the canonical service.*
// request types before calling the transport, but those still do not implement
// proto.Message, so they must be relayed through the generated Crypto proto
// service rather than the untyped conn.Invoke path.
func isCryptoService(svc string) bool {
	return strings.HasPrefix(svc, cryptoServicePrefix)
}

// cryptoHandler relays the crypto SDK client's canonical service.* requests to
// the generated Crypto proto service. It delegates the struct<->protobuf
// conversion and RPC to the pkg/api/grpc Client, which wraps the transport's
// existing connection via NewClientWithConn so no second connection is dialed
// and no conversion logic is forked.
type cryptoHandler struct {
	conn   *grpc.ClientConn
	client *apigrpc.Client
}

// newCryptoHandler creates a new crypto handler bound to conn.
func newCryptoHandler(conn *grpc.ClientConn) (*cryptoHandler, error) {
	client, err := apigrpc.NewClientWithConn(conn)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "grpc.newCryptoHandler",
			"failed to wrap connection")
	}
	return &cryptoHandler{conn: conn, client: client}, nil
}

// cryptoHandlerFunc handles a single crypto method.
type cryptoHandlerFunc func(h *cryptoHandler, ctx context.Context, req interface{}) (interface{}, error)

// cryptoHandlerMap maps method names to handler functions for O(1) dispatch.
// Verbs without a Crypto proto / pkg/api/grpc Client analogue (EncryptBatch,
// DecryptBatch, GenerateWrappedDataKey) return a clear unimplemented error
// rather than falling through to the untyped conn.Invoke path.
var cryptoHandlerMap = map[string]cryptoHandlerFunc{
	"Encrypt":         (*cryptoHandler).handleEncrypt,
	"Decrypt":         (*cryptoHandler).handleDecrypt,
	"Sign":            (*cryptoHandler).handleSign,
	"Verify":          (*cryptoHandler).handleVerify,
	"Rewrap":          (*cryptoHandler).handleRewrap,
	"GenerateDataKey": (*cryptoHandler).handleGenerateDataKey,
	"GenerateHMAC":    (*cryptoHandler).handleGenerateHMAC,
	"VerifyHMAC":      (*cryptoHandler).handleVerifyHMAC,
	"GenerateRandom":  (*cryptoHandler).handleGenerateRandom,
	"Hash":            (*cryptoHandler).handleHash,
}

// handleRequest routes a crypto request to the appropriate handler based on the
// method name.
func (h *cryptoHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	handler, ok := cryptoHandlerMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("cryptoHandler.handleRequest",
			"method not exposed by Crypto gRPC proto: "+method+
				" (use the REST transport for this verb)")
	}
	return handler(h, ctx, req)
}

func (h *cryptoHandler) handleEncrypt(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.EncryptRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleEncrypt",
			"invalid request type, expected *service.EncryptRequest")
	}
	return h.client.KMSEncrypt(ctx, r)
}

func (h *cryptoHandler) handleDecrypt(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.DecryptRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleDecrypt",
			"invalid request type, expected *service.DecryptRequest")
	}
	return h.client.KMSDecrypt(ctx, r)
}

func (h *cryptoHandler) handleSign(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.SignRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleSign",
			"invalid request type, expected *service.SignRequest")
	}
	return h.client.KMSSign(ctx, r)
}

func (h *cryptoHandler) handleVerify(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.VerifyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleVerify",
			"invalid request type, expected *service.VerifyRequest")
	}
	return h.client.KMSVerify(ctx, r)
}

func (h *cryptoHandler) handleRewrap(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.RewrapRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleRewrap",
			"invalid request type, expected *service.RewrapRequest")
	}
	return h.client.KMSRewrap(ctx, r)
}

func (h *cryptoHandler) handleGenerateDataKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GenerateDataKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleGenerateDataKey",
			"invalid request type, expected *service.GenerateDataKeyRequest")
	}
	return h.client.KMSGenerateDataKey(ctx, r)
}

func (h *cryptoHandler) handleGenerateHMAC(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.HMACRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleGenerateHMAC",
			"invalid request type, expected *service.HMACRequest")
	}
	return h.client.KMSGenerateHMAC(ctx, r)
}

func (h *cryptoHandler) handleVerifyHMAC(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.VerifyHMACRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleVerifyHMAC",
			"invalid request type, expected *service.VerifyHMACRequest")
	}
	return h.client.KMSVerifyHMAC(ctx, r)
}

func (h *cryptoHandler) handleGenerateRandom(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GenerateRandomRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleGenerateRandom",
			"invalid request type, expected *service.GenerateRandomRequest")
	}
	return h.client.KMSGenerateRandom(ctx, r)
}

func (h *cryptoHandler) handleHash(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.HashRequest)
	if !ok {
		return nil, errors.NewInvalidInput("cryptoHandler.handleHash",
			"invalid request type, expected *service.HashRequest")
	}
	return h.client.KMSHash(ctx, r)
}
