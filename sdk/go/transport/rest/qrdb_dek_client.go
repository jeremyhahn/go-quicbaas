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

package rest

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/url"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBDEKClient calls go-qrdb's tenant DEK keyring routes through this
// transport, so the platform bearer and headers ride every call. go-qrdb's
// own REST sub-client sets no Authorization header. The data verbs (encrypt,
// decrypt, rewrap) refuse any hop that is not the configured https origin and
// follow no redirect, and every verb refuses to send the bearer in plaintext
// or across hosts.
type QRDBDEKClient struct {
	t *Transport
}

var _ qrdbtransport.DEKClient = (*QRDBDEKClient)(nil)

// QRDBDEK returns the go-qrdb tenant DEK client bound to this transport.
func (t *Transport) QRDBDEK() *QRDBDEKClient {
	return &QRDBDEKClient{t: t}
}

const qrdbDEKOp = "rest.QRDBDEKClient"

func qrdbDEKNilRequest(method string) error {
	return errors.NewInvalidInput(qrdbDEKOp+"."+method, "request is nil")
}

// qrdbDEKCtx marks ctx for the tenant bearer guards: the platform bearer
// never goes out on a plaintext hop, nor follows a redirect to http or to
// another host.
func qrdbDEKCtx(ctx context.Context) context.Context { return withTenantBearerHop(ctx) }

// qrdbDEKSecretCtx also marks ctx as carrying key-protected data (plaintext
// in an encrypt request or a decrypt response, a ciphertext being rewrapped):
// every hop must be the configured https origin, and no redirect is followed.
func qrdbDEKSecretCtx(ctx context.Context, verb string) context.Context {
	return withSecretHop(withTenantBearerHop(ctx), verb)
}

// qrdbDEKDo runs one tenant DEK call. A refused bearer hop (ErrInsecureRedirect
// or ErrPlaintextBearer) comes back as a non-retryable CodeForbidden error that
// still wraps the sentinel, as the tenant barrier verbs return it.
func (c *QRDBDEKClient) qrdbDEKDo(ctx context.Context, verb, method, path string, reqBody, result any) error {
	err := doJSONResult(ctx, c.t, qrdbDEKOp, method, path, reqBody, result)
	switch {
	case err == nil:
		return nil
	case stderrors.Is(err, ErrInsecureRedirect):
		return errors.Wrap(err, errors.CodeForbidden, qrdbDEKOp, verb+": refused an insecure redirect")
	case stderrors.Is(err, ErrPlaintextBearer):
		return errors.Wrap(err, errors.CodeForbidden, qrdbDEKOp, verb+": refused to send the bearer over plaintext")
	}
	return err
}

// TenantDEKStatus maps to GET /v1/dek/status.
func (c *QRDBDEKClient) TenantDEKStatus(ctx context.Context, req *qrdbtransport.TenantDEKRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKStatus")
	}
	path := "/v1/dek/status"
	if req.TenantID != "" {
		path += "?tenant_id=" + url.QueryEscape(req.TenantID)
	}
	var resp qrdbtransport.TenantDEKStatusResponse
	if err := c.qrdbDEKDo(qrdbDEKCtx(ctx), "TenantDEKStatus", http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKRotate maps to POST /v1/dek/rotate.
func (c *QRDBDEKClient) TenantDEKRotate(ctx context.Context, req *qrdbtransport.TenantDEKRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKRotate")
	}
	var resp qrdbtransport.TenantDEKStatusResponse
	if err := c.qrdbDEKDo(qrdbDEKCtx(ctx), "TenantDEKRotate", http.MethodPost, "/v1/dek/rotate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKRetire maps to POST /v1/dek/retire.
func (c *QRDBDEKClient) TenantDEKRetire(ctx context.Context, req *qrdbtransport.TenantDEKRetireRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKRetire")
	}
	var resp qrdbtransport.TenantDEKStatusResponse
	if err := c.qrdbDEKDo(qrdbDEKCtx(ctx), "TenantDEKRetire", http.MethodPost, "/v1/dek/retire", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKEncrypt maps to POST /v1/dek/encrypt.
func (c *QRDBDEKClient) TenantDEKEncrypt(ctx context.Context, req *qrdbtransport.TenantDEKEncryptRequest) (*qrdbtransport.TenantDEKEncryptResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKEncrypt")
	}
	var resp qrdbtransport.TenantDEKEncryptResponse
	if err := c.qrdbDEKDo(qrdbDEKSecretCtx(ctx, "TenantDEKEncrypt"), "TenantDEKEncrypt", http.MethodPost, "/v1/dek/encrypt", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKDecrypt maps to POST /v1/dek/decrypt.
func (c *QRDBDEKClient) TenantDEKDecrypt(ctx context.Context, req *qrdbtransport.TenantDEKDecryptRequest) (*qrdbtransport.TenantDEKDecryptResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKDecrypt")
	}
	var resp qrdbtransport.TenantDEKDecryptResponse
	if err := c.qrdbDEKDo(qrdbDEKSecretCtx(ctx, "TenantDEKDecrypt"), "TenantDEKDecrypt", http.MethodPost, "/v1/dek/decrypt", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKRewrap maps to POST /v1/dek/rewrap.
func (c *QRDBDEKClient) TenantDEKRewrap(ctx context.Context, req *qrdbtransport.TenantDEKRewrapRequest) (*qrdbtransport.TenantDEKRewrapResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKRewrap")
	}
	var resp qrdbtransport.TenantDEKRewrapResponse
	if err := c.qrdbDEKDo(qrdbDEKSecretCtx(ctx, "TenantDEKRewrap"), "TenantDEKRewrap", http.MethodPost, "/v1/dek/rewrap", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
