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
	"net/http"
	"net/url"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBDEKClient calls go-qrdb's tenant DEK keyring routes through this
// transport, so the platform bearer and headers ride every call. go-qrdb's
// own REST sub-client sets no Authorization header.
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
	if err := doJSONResult(ctx, c.t, qrdbDEKOp, http.MethodGet, path, nil, &resp); err != nil {
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
	if err := doJSONResult(ctx, c.t, qrdbDEKOp, http.MethodPost, "/v1/dek/rotate", req, &resp); err != nil {
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
	if err := doJSONResult(ctx, c.t, qrdbDEKOp, http.MethodPost, "/v1/dek/retire", req, &resp); err != nil {
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
	if err := doJSONResult(ctx, c.t, qrdbDEKOp, http.MethodPost, "/v1/dek/encrypt", req, &resp); err != nil {
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
	if err := doJSONResult(ctx, c.t, qrdbDEKOp, http.MethodPost, "/v1/dek/decrypt", req, &resp); err != nil {
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
	if err := doJSONResult(ctx, c.t, qrdbDEKOp, http.MethodPost, "/v1/dek/rewrap", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
