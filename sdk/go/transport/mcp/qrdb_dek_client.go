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
	stderrors "errors"
	"strings"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBDEKClient calls go-qrdb's tenant DEK keyring tools over this
// transport's MCP session, so every call, the session's initialize included,
// carries the platform credentials the transport was built with. go-qrdb's
// own MCP sub-client sends no Authorization header.
//
// Because every verb sends that bearer, and the data verbs send plaintext or
// return it, every verb refuses a non-https endpoint before the session is
// initialized, and every hop of its initialize, authenticate and tool call
// must be https and name the configured host. Encrypt, decrypt and rewrap
// also follow no redirect at all. A refusal is a non-retryable CodeForbidden
// error wrapping a *DEKHopError.
type QRDBDEKClient struct {
	t *Transport
}

var _ qrdbtransport.DEKClient = (*QRDBDEKClient)(nil)

// QRDBDEK returns a go-qrdb tenant DEK client bound to this transport.
func (t *Transport) QRDBDEK() *QRDBDEKClient {
	return &QRDBDEKClient{t: t}
}

// call runs one go-qrdb keyring tool and decodes its JSON result into out.
// data is set for the verbs that carry key-protected data (encrypt, decrypt,
// rewrap). A non-https endpoint is refused before anything is sent; then the
// session initialize, the authentication and the tool call all run on a ctx
// marked for pinHopRoundTripper's DEK rules.
func (c *QRDBDEKClient) call(ctx context.Context, method, tool string, args map[string]interface{}, out interface{}, data bool) error {
	t := c.t
	op := "mcp.QRDBDEK." + method
	if t.closed.Load() {
		return errors.NewConnection(op, "transport is closed")
	}
	if !strings.HasPrefix(t.endpoint, "https://") {
		return qrdbDEKRefusal(op, &DEKHopError{Verb: method, Reason: dekHopReasonPlaintext})
	}
	gctx := withDEKHopGuard(ctx, method, data)
	if err := t.ensureInitialized(gctx); err != nil {
		return qrdbDEKHopErr(op, err)
	}
	if err := t.ensureAuthenticated(gctx); err != nil {
		return qrdbDEKHopErr(op, err)
	}
	result, err := t.callTool(gctx, tool, args)
	if err != nil {
		return qrdbDEKHopErr(op, err)
	}
	return decodeQRDBToolResult(op, result, out)
}

// qrdbDEKRefusal returns a refused hop as a non-retryable CodeForbidden error
// that still wraps the *DEKHopError, as the REST keyring returns its refusals.
func qrdbDEKRefusal(op string, hopErr *DEKHopError) error {
	return errors.Wrap(hopErr, errors.CodeForbidden, op, hopErr.Verb+": "+hopErr.Reason)
}

// qrdbDEKHopErr turns a refused hop anywhere in err's chain into the
// CodeForbidden refusal, and returns any other error unchanged.
func qrdbDEKHopErr(op string, err error) error {
	var hopErr *DEKHopError
	if stderrors.As(err, &hopErr) {
		return qrdbDEKRefusal(op, hopErr)
	}
	return err
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
	if err := c.call(ctx, "TenantDEKStatus", "qrdb_dek_status", qrdbDEKArgs(req.TenantID), &resp, false); err != nil {
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
	if err := c.call(ctx, "TenantDEKRotate", "qrdb_dek_rotate", qrdbDEKArgs(req.TenantID), &resp, false); err != nil {
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
	if err := c.call(ctx, "TenantDEKRetire", "qrdb_dek_retire", args, &resp, false); err != nil {
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
	if err := c.call(ctx, "TenantDEKEncrypt", "qrdb_dek_encrypt", qrdbDEKDataArgs(req.TenantID, "plaintext", req.Plaintext, req.AAD), &resp, true); err != nil {
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
	if err := c.call(ctx, "TenantDEKDecrypt", "qrdb_dek_decrypt", qrdbDEKDataArgs(req.TenantID, "ciphertext", req.Ciphertext, req.AAD), &resp, true); err != nil {
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
	if err := c.call(ctx, "TenantDEKRewrap", "qrdb_dek_rewrap", qrdbDEKDataArgs(req.TenantID, "ciphertext", req.Ciphertext, req.AAD), &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}
