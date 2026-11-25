// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBCredentialClient is go-qrdb's CredentialClient served over this
// quicbaas MCP transport. It calls go-qrdb's native qrdb_credential_* tools
// on the unified server through the transport's own MCP session, so it
// reuses the transport's MCP token or login credentials (the platform
// bearer) and opens no second session. go-qrdb's MCP sub-client sends no
// platform bearer, so an operational node refused its initialize with 401.
// Tool names, argument names and response decoding follow go-qrdb's own
// MCP transport (sdk/go/transport/mcp/transport_credentials.go).
type QRDBCredentialClient struct {
	t *Transport
}

var _ qrdbtransport.CredentialClient = (*QRDBCredentialClient)(nil)

// QRDBCredentials returns go-qrdb's CredentialClient over this transport.
func (t *Transport) QRDBCredentials() *QRDBCredentialClient {
	return &QRDBCredentialClient{t: t}
}

const qrdbCredentialComponent = "mcp.QRDBCredentialClient"

func refuseNilQRDBCredentialRequest(verb string) error {
	return errors.New(errors.CodeInvalidInput, qrdbCredentialComponent, verb+": request is nil")
}

// decodeQRDBCredentialJSON unmarshals a qrdb_credential_* tool's text into
// out.
func decodeQRDBCredentialJSON(verb, text string, out any) error {
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return errors.Wrap(err, errors.CodeInternal, qrdbCredentialComponent, verb+": failed to decode response")
	}
	return nil
}

// CredentialSubmit calls qrdb_credential_submit. The value travels
// base64-encoded (RFC 4648) so binary content round-trips.
func (c *QRDBCredentialClient) CredentialSubmit(ctx context.Context, req *qrdbtransport.CredentialSubmitRequest) error {
	if req == nil {
		return refuseNilQRDBCredentialRequest("CredentialSubmit")
	}
	_, err := c.t.callQRDBTool(ctx, "qrdb_credential_submit", map[string]interface{}{
		"name":  req.Name,
		"value": base64.StdEncoding.EncodeToString(req.Value),
	})
	return err
}

// CredentialStrategy calls qrdb_credential_strategy.
func (c *QRDBCredentialClient) CredentialStrategy(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error) {
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_strategy", nil)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CredentialStrategyResponse
	if err := decodeQRDBCredentialJSON("CredentialStrategy", text, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialDelete calls qrdb_credential_delete.
func (c *QRDBCredentialClient) CredentialDelete(ctx context.Context, name string) error {
	_, err := c.t.callQRDBTool(ctx, "qrdb_credential_delete", map[string]interface{}{
		"name": name,
	})
	return err
}

// CredentialList calls qrdb_credential_list. req is reserved for future
// filters, as in go-qrdb, and is not sent.
func (c *QRDBCredentialClient) CredentialList(ctx context.Context, _ *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_list", nil)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CredentialListResponse
	if err := decodeQRDBCredentialJSON("CredentialList", text, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialRotate calls qrdb_credential_rotate. new_value travels
// base64-encoded (RFC 4648).
func (c *QRDBCredentialClient) CredentialRotate(ctx context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
	if req == nil {
		return nil, refuseNilQRDBCredentialRequest("CredentialRotate")
	}
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_rotate", map[string]interface{}{
		"name":                 req.Name,
		"new_value":            base64.StdEncoding.EncodeToString(req.NewValue),
		"grace_period_seconds": req.GracePeriodSeconds,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CredentialRotateResponse
	if err := decodeQRDBCredentialJSON("CredentialRotate", text, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialRetrieveVersion calls qrdb_credential_retrieve_version. Version
// 0 is the current version.
func (c *QRDBCredentialClient) CredentialRetrieveVersion(ctx context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
	if req == nil {
		return nil, refuseNilQRDBCredentialRequest("CredentialRetrieveVersion")
	}
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_retrieve_version", map[string]interface{}{
		"name":    req.Name,
		"version": req.Version,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CredentialRetrieveVersionResponse
	if err := decodeQRDBCredentialJSON("CredentialRetrieveVersion", text, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialListVersions calls qrdb_credential_list_versions.
func (c *QRDBCredentialClient) CredentialListVersions(ctx context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
	if req == nil {
		return nil, refuseNilQRDBCredentialRequest("CredentialListVersions")
	}
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_list_versions", map[string]interface{}{
		"name": req.Name,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CredentialListVersionsResponse
	if err := decodeQRDBCredentialJSON("CredentialListVersions", text, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialGCExpired calls qrdb_credential_gc_expired.
func (c *QRDBCredentialClient) CredentialGCExpired(ctx context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
	if req == nil {
		return nil, refuseNilQRDBCredentialRequest("CredentialGCExpired")
	}
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_gc_expired", map[string]interface{}{
		"name": req.Name,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CredentialGCResponse
	if err := decodeQRDBCredentialJSON("CredentialGCExpired", text, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialStore calls qrdb_credential_store. The value travels
// base64-encoded (RFC 4648).
func (c *QRDBCredentialClient) CredentialStore(ctx context.Context, name string, value []byte) error {
	_, err := c.t.callQRDBTool(ctx, "qrdb_credential_store", map[string]interface{}{
		"name":  name,
		"value": base64.StdEncoding.EncodeToString(value),
	})
	return err
}

// CredentialRetrieve calls qrdb_credential_retrieve, which answers the value
// base64-encoded (RFC 4648), and decodes it back to raw bytes.
func (c *QRDBCredentialClient) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	text, err := c.t.callQRDBTool(ctx, "qrdb_credential_retrieve", map[string]interface{}{
		"name": name,
	})
	if err != nil {
		return nil, err
	}
	value, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, qrdbCredentialComponent, "CredentialRetrieve: failed to decode value")
	}
	return value, nil
}
