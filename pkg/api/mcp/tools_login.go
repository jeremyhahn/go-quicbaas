//go:build mcp

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

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Unified Login Ceremony MCP tools — quicbaas_login_begin / quicbaas_login_finish.
//
// Both tools delegate to the QuicBaaS LoginCeremony() facade. The verified
// mTLS peer certificate is threaded onto the context by the MCP HTTP handler
// (server.go) from the TLS handshake; the facade derives the mtls factor
// lookup key from it (never from a client-supplied value).
// =============================================================================

// loginServiceUnavailableQBError is the canonical QBError returned when the
// login ceremony facade is not wired.
var loginServiceUnavailableQBError = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.login",
	Err:  &LoginServiceUnavailableError{},
}

// LoginServiceUnavailableError is returned by the login tools when the
// LoginCeremony() facade is unavailable.
type LoginServiceUnavailableError struct{}

// Error implements the error interface. The message contains "unavailable" so
// the MCP error mapper's fallback classifies it as ServiceUnavailable.
func (e *LoginServiceUnavailableError) Error() string {
	return "login ceremony unavailable: not configured"
}

// registerLoginTools registers the unified login ceremony MCP tools.
func (r *ToolRegistry) registerLoginTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_login_begin",
		Description: "Begin the unified login ceremony for a factor (mtls / fido2 / piv); returns the server challenge.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"type":      map[string]interface{}{"type": "string", "description": "Factor type: mtls, fido2, or piv."},
				"hint":      map[string]interface{}{"type": "string", "description": "Optional user hint (username/tenant)."},
				"tenant_id": map[string]interface{}{"type": "string", "description": "Optional tenant scope."},
			},
			"required": []string{"type"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_login_finish",
		Description: "Finish the unified login ceremony by submitting the factor proof; returns the issued bearer. For mtls the server derives the lookup key from the verified peer certificate.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id":      map[string]interface{}{"type": "string", "description": "Session id from begin step."},
				"type":            map[string]interface{}{"type": "string", "description": "Factor type: mtls, fido2, or piv."},
				"lookup_key":      map[string]interface{}{"type": "string", "description": "Base64 lookup key (ignored for mtls over a network transport)."},
				"alt_lookup_keys": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Base64 fallback lookup keys tried in order after lookup_key misses (ignored for mtls over a network transport)."},
				"signature":       map[string]interface{}{"type": "string", "description": "Base64 proof signature."},
				"method_metadata": map[string]interface{}{"type": "object", "description": "Factor-specific extras."},
			},
			"required": []string{"session_id", "type"},
		},
	})
}

// loginToolNames is the set of login-surface tool names.
var loginToolNames = map[string]bool{
	"quicbaas_login_begin":  true,
	"quicbaas_login_finish": true,
}

// isLoginTool reports whether the tool name belongs to the login surface.
func isLoginTool(name string) bool {
	return loginToolNames[name]
}

// executeLoginTool dispatches a login-surface call.
func (r *ToolRegistry) executeLoginTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if r.service == nil {
		return nil, loginServiceUnavailableQBError
	}
	login := r.service.LoginCeremony()
	if login == nil {
		return nil, loginServiceUnavailableQBError
	}
	switch name {
	case "quicbaas_login_begin":
		return executeLoginBegin(ctx, login, args)
	case "quicbaas_login_finish":
		return executeLoginFinish(ctx, login, args)
	default:
		return nil, loginServiceUnavailableQBError
	}
}

// executeLoginBegin implements quicbaas_login_begin.
func executeLoginBegin(ctx context.Context, login service.LoginServicer, args map[string]interface{}) (interface{}, error) {
	req := &qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorType(getString(args, "type")),
		Hint:     getString(args, "hint"),
		TenantID: getString(args, "tenant_id"),
	}
	resp, err := login.BeginLogin(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeLoginFinish implements quicbaas_login_finish. The verified peer cert
// is already on the context; the facade enforces the mTLS security model.
func executeLoginFinish(ctx context.Context, login service.LoginServicer, args map[string]interface{}) (interface{}, error) {
	req := &qrdbtransport.FinishLoginRequest{
		SessionID:      getString(args, "session_id"),
		Type:           qrdbtransport.FactorType(getString(args, "type")),
		LookupKey:      decodeLoginBase64(args, "lookup_key"),
		AltLookupKeys:  decodeLoginBase64Array(args, "alt_lookup_keys"),
		Signature:      decodeLoginBase64(args, "signature"),
		MethodMetadata: decodeLoginStringMap(args, "method_metadata"),
	}
	resp, err := login.FinishLogin(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// decodeLoginBase64 decodes a base64-encoded byte field from the tool
// arguments. The SDK MCP transport JSON-encodes []byte fields as base64
// strings. Returns nil when absent or undecodable.
func decodeLoginBase64(args map[string]interface{}, key string) []byte {
	s := getString(args, key)
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// decodeLoginBase64Array decodes an array of base64-encoded byte fields from
// the tool arguments. The SDK MCP transport JSON-encodes [][]byte fields as
// arrays of base64 strings. Returns nil when absent; undecodable entries are
// skipped so a malformed fallback key cannot abort an otherwise valid login.
func decodeLoginBase64Array(args map[string]interface{}, key string) [][]byte {
	raw, ok := args[key].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeLoginStringMap extracts a string->string map from the tool arguments.
func decodeLoginStringMap(args map[string]interface{}, key string) map[string]string {
	raw, ok := args[key].(map[string]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
