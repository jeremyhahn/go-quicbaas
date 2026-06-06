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

// crypto_handler.go provides typed REST routing for the quicbaas.Crypto
// service. The generic dispatch path mapped Crypto methods to endpoint
// templates with a literal {name} placeholder and returned an opaque map; this
// handler substitutes the concrete key name from the typed service request and
// decodes responses directly into the service.*Response types the SDK Crypto
// client expects.
//
// Methods without a server route (GenerateWrappedDataKey, GenerateHMAC,
// VerifyHMAC) fall through to the generic dispatch path.

package rest

import (
	"context"
	"net/http"
	"net/url"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// cryptoServicePrefix is the gRPC-style service identifier the SDK uses to route
// Crypto requests to this handler.
const cryptoServicePrefix = "quicbaas.Crypto"

// isCryptoService reports whether the supplied service name resolves to the
// Crypto service.
func isCryptoService(service string) bool {
	return trimRESTServicePrefix(service) == "Crypto"
}

// cryptoRESTHandler delegates Crypto service calls to the quicbaasd REST
// endpoints.
type cryptoRESTHandler struct {
	transport *Transport
}

// newCryptoRESTHandler constructs a handler bound to t.
func newCryptoRESTHandler(t *Transport) *cryptoRESTHandler {
	return &cryptoRESTHandler{transport: t}
}

// handleRequest dispatches the Crypto service method to the matching REST
// endpoint. The SDK Crypto client converts its public requests into the
// service.*Request types before dispatch, so requests arrive already typed.
func (h *cryptoRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "Encrypt":
		return h.handleEncrypt(ctx, req)
	case "Decrypt":
		return h.handleDecrypt(ctx, req)
	case "Rewrap":
		return h.handleRewrap(ctx, req)
	case "Sign":
		return h.handleSign(ctx, req)
	case "Verify":
		return h.handleVerify(ctx, req)
	case "Hash":
		return h.handleHash(ctx, req)
	case "GenerateRandom":
		return h.handleGenerateRandom(ctx, req)
	case "GenerateDataKey":
		return h.handleGenerateDataKey(ctx, req)
	case "EncryptBatch":
		return h.handleEncryptBatch(ctx, req)
	case "DecryptBatch":
		return h.handleDecryptBatch(ctx, req)
	default:
		return h.transport.requestWithRetry(ctx, cryptoServicePrefix, method, req)
	}
}

func (h *cryptoRESTHandler) handleEncrypt(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.EncryptRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.Encrypt", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.Encrypt", "key ID is required")
	}
	endpoint := "/api/v1/crypto/encrypt/" + url.PathEscape(r.KeyID)
	var resp service.EncryptResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleDecrypt(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.DecryptRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.Decrypt", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.Decrypt", "key ID is required")
	}
	endpoint := "/api/v1/crypto/decrypt/" + url.PathEscape(r.KeyID)
	var resp service.DecryptResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleRewrap(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.RewrapRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.Rewrap", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.Rewrap", "key ID is required")
	}
	endpoint := "/api/v1/crypto/rewrap/" + url.PathEscape(r.KeyID)
	var resp service.RewrapResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleSign(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.SignRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.Sign", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.Sign", "key ID is required")
	}
	endpoint := "/api/v1/crypto/sign/" + url.PathEscape(r.KeyID)
	var resp service.SignResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleVerify(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.VerifyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.Verify", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.Verify", "key ID is required")
	}
	endpoint := "/api/v1/crypto/verify/" + url.PathEscape(r.KeyID)
	var resp service.VerifyResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleHash(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.HashRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.Hash", "request is required")
	}
	var resp service.HashResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/crypto/hash", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleGenerateRandom(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GenerateRandomRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.GenerateRandom", "request is required")
	}
	var resp service.GenerateRandomResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/crypto/random", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleGenerateDataKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GenerateDataKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.GenerateDataKey", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.GenerateDataKey", "key ID is required")
	}
	endpoint := "/api/v1/crypto/datakey/" + url.PathEscape(r.KeyID)
	var resp service.GenerateDataKeyResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleEncryptBatch(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.EncryptBatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.EncryptBatch", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.EncryptBatch", "key ID is required")
	}
	endpoint := "/api/v1/crypto/encrypt/" + url.PathEscape(r.KeyID) + "/batch"
	var resp service.EncryptBatchResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *cryptoRESTHandler) handleDecryptBatch(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.DecryptBatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.crypto.DecryptBatch", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.crypto.DecryptBatch", "key ID is required")
	}
	endpoint := "/api/v1/crypto/decrypt/" + url.PathEscape(r.KeyID) + "/batch"
	var resp service.DecryptBatchResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
