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

package mcp

import (
	"context"
	"encoding/base64"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBDEKClient calls go-qrdb's tenant DEK keyring tools over this
// transport's MCP session, so every call, the session's initialize included,
// carries the platform credentials the transport was built with. go-qrdb's
// own MCP sub-client sends no Authorization header.
type QRDBDEKClient struct {
	t *Transport
}

var _ qrdbtransport.DEKClient = (*QRDBDEKClient)(nil)

// QRDBDEK returns a go-qrdb tenant DEK client bound to this transport.
func (t *Transport) QRDBDEK() *QRDBDEKClient {
	return &QRDBDEKClient{t: t}
}

// call runs one go-qrdb keyring tool and decodes its JSON result into out.
func (c *QRDBDEKClient) call(ctx context.Context, method, tool string, args map[string]interface{}, out interface{}) error {
	return (&QRDBEmbeddingClient{t: c.t}).call(ctx, "mcp.QRDBDEK."+method, tool, args, out)
}

func qrdbDEKNilRequest(method string) error {
	return errors.NewInvalidInput("mcp.QRDBDEK."+method, "request is nil")
}

// qrdbDEKArgs returns the tool arguments carrying the optional tenant.
func qrdbDEKArgs(tenantID string) map[string]interface{} {
	args := map[string]interface{}{}
	if tenantID != "" {
		args["tenant_id"] = tenantID
	}
	return args
}

// qrdbDEKDataArgs returns the tool arguments for a data-carrying keyring
// tool: the data and its associated data travel base64-encoded, as go-qrdb's
// keyring tools expect.
func qrdbDEKDataArgs(tenantID, field string, data, aad []byte) map[string]interface{} {
	args := qrdbDEKArgs(tenantID)
	args[field] = base64.StdEncoding.EncodeToString(data)
	if len(aad) > 0 {
		args["aad"] = base64.StdEncoding.EncodeToString(aad)
	}
	return args
}

// TenantDEKStatus calls qrdb_dek_status.
func (c *QRDBDEKClient) TenantDEKStatus(ctx context.Context, req *qrdbtransport.TenantDEKRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKStatus")
	}
	var resp qrdbtransport.TenantDEKStatusResponse
	if err := c.call(ctx, "TenantDEKStatus", "qrdb_dek_status", qrdbDEKArgs(req.TenantID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKRotate calls qrdb_dek_rotate.
func (c *QRDBDEKClient) TenantDEKRotate(ctx context.Context, req *qrdbtransport.TenantDEKRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKRotate")
	}
	var resp qrdbtransport.TenantDEKStatusResponse
	if err := c.call(ctx, "TenantDEKRotate", "qrdb_dek_rotate", qrdbDEKArgs(req.TenantID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKRetire calls qrdb_dek_retire.
func (c *QRDBDEKClient) TenantDEKRetire(ctx context.Context, req *qrdbtransport.TenantDEKRetireRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKRetire")
	}
	args := qrdbDEKArgs(req.TenantID)
	args["version"] = req.Version
	var resp qrdbtransport.TenantDEKStatusResponse
	if err := c.call(ctx, "TenantDEKRetire", "qrdb_dek_retire", args, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKEncrypt calls qrdb_dek_encrypt.
func (c *QRDBDEKClient) TenantDEKEncrypt(ctx context.Context, req *qrdbtransport.TenantDEKEncryptRequest) (*qrdbtransport.TenantDEKEncryptResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKEncrypt")
	}
	var resp qrdbtransport.TenantDEKEncryptResponse
	if err := c.call(ctx, "TenantDEKEncrypt", "qrdb_dek_encrypt", qrdbDEKDataArgs(req.TenantID, "plaintext", req.Plaintext, req.AAD), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKDecrypt calls qrdb_dek_decrypt.
func (c *QRDBDEKClient) TenantDEKDecrypt(ctx context.Context, req *qrdbtransport.TenantDEKDecryptRequest) (*qrdbtransport.TenantDEKDecryptResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKDecrypt")
	}
	var resp qrdbtransport.TenantDEKDecryptResponse
	if err := c.call(ctx, "TenantDEKDecrypt", "qrdb_dek_decrypt", qrdbDEKDataArgs(req.TenantID, "ciphertext", req.Ciphertext, req.AAD), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TenantDEKRewrap calls qrdb_dek_rewrap.
func (c *QRDBDEKClient) TenantDEKRewrap(ctx context.Context, req *qrdbtransport.TenantDEKRewrapRequest) (*qrdbtransport.TenantDEKRewrapResponse, error) {
	if req == nil {
		return nil, qrdbDEKNilRequest("TenantDEKRewrap")
	}
	var resp qrdbtransport.TenantDEKRewrapResponse
	if err := c.call(ctx, "TenantDEKRewrap", "qrdb_dek_rewrap", qrdbDEKDataArgs(req.TenantID, "ciphertext", req.Ciphertext, req.AAD), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
