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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fakeLoginServicer is a test double for service.LoginServicer.
type fakeLoginServicer struct {
	beginFunc  func(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error)
	finishFunc func(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*service.FinishLoginResult, error)
}

func (f *fakeLoginServicer) BeginLogin(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return f.beginFunc(ctx, req)
}

func (f *fakeLoginServicer) FinishLogin(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*service.FinishLoginResult, error) {
	return f.finishFunc(ctx, req)
}

// loginMockService embeds mockService and wires a LoginServicer.
type loginMockService struct {
	*mockService
	login service.LoginServicer
}

func (m *loginMockService) LoginCeremony() service.LoginServicer { return m.login }

func newLoginRegistry(t *testing.T, login service.LoginServicer) *ToolRegistry {
	t.Helper()
	return &ToolRegistry{
		service: &loginMockService{mockService: &mockService{}, login: login},
		logger:  newTestLogger(t),
	}
}

// =============================================================================
// LoginServiceUnavailableError
// =============================================================================

func TestLoginServiceUnavailableError(t *testing.T) {
	err := &LoginServiceUnavailableError{}
	assert.Contains(t, err.Error(), "unavailable")
}

// =============================================================================
// isLoginTool
// =============================================================================

func TestIsLoginTool(t *testing.T) {
	assert.True(t, isLoginTool("quicbaas_login_begin"))
	assert.True(t, isLoginTool("quicbaas_login_finish"))
	assert.False(t, isLoginTool("quicbaas_kms_generate"))
}

// =============================================================================
// executeLoginTool dispatch + unavailable paths
// =============================================================================

func TestExecuteLoginTool_ServiceNil(t *testing.T) {
	r := &ToolRegistry{service: nil, logger: newTestLogger(t)}
	_, err := r.executeLoginTool(context.Background(), "quicbaas_login_begin", nil)
	require.ErrorIs(t, err, loginServiceUnavailableQBError)
}

func TestExecuteLoginTool_LoginCeremonyNil(t *testing.T) {
	// mockService.LoginCeremony() returns nil.
	r := &ToolRegistry{service: &mockService{}, logger: newTestLogger(t)}
	_, err := r.executeLoginTool(context.Background(), "quicbaas_login_begin", nil)
	require.ErrorIs(t, err, loginServiceUnavailableQBError)
}

func TestExecuteLoginTool_UnknownTool(t *testing.T) {
	r := newLoginRegistry(t, &fakeLoginServicer{})
	_, err := r.executeLoginTool(context.Background(), "quicbaas_login_unknown", nil)
	require.ErrorIs(t, err, loginServiceUnavailableQBError)
}

func TestExecuteLoginTool_DispatchBegin(t *testing.T) {
	called := false
	login := &fakeLoginServicer{
		beginFunc: func(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
			called = true
			return &qrdbtransport.BeginLoginResponse{SessionID: "s1"}, nil
		},
	}
	r := newLoginRegistry(t, login)
	resp, err := r.executeLoginTool(context.Background(), "quicbaas_login_begin", map[string]interface{}{"type": "mtls"})
	require.NoError(t, err)
	require.True(t, called)
	begin := resp.(*qrdbtransport.BeginLoginResponse)
	assert.Equal(t, "s1", begin.SessionID)
}

func TestExecuteLoginTool_DispatchFinish(t *testing.T) {
	login := &fakeLoginServicer{
		finishFunc: func(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*service.FinishLoginResult, error) {
			return &service.FinishLoginResult{FinishLoginResponse: &qrdbtransport.FinishLoginResponse{Bearer: "tok"}, RefreshToken: "r-tok"}, nil
		},
	}
	r := newLoginRegistry(t, login)
	resp, err := r.executeLoginTool(context.Background(), "quicbaas_login_finish",
		map[string]interface{}{"session_id": "s1", "type": "mtls"})
	require.NoError(t, err)
	finish := resp.(*service.FinishLoginResult)
	assert.Equal(t, "tok", finish.Bearer)
	assert.Equal(t, "r-tok", finish.RefreshToken)
}

// =============================================================================
// executeLoginBegin
// =============================================================================

func TestExecuteLoginBegin_PropagatesArgsAndError(t *testing.T) {
	t.Run("maps_args", func(t *testing.T) {
		login := &fakeLoginServicer{
			beginFunc: func(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
				assert.Equal(t, qrdbtransport.FactorType("fido2"), req.Type)
				assert.Equal(t, "alice", req.Hint)
				assert.Equal(t, "t1", req.TenantID)
				return &qrdbtransport.BeginLoginResponse{SessionID: "s2"}, nil
			},
		}
		resp, err := executeLoginBegin(context.Background(), login, map[string]interface{}{
			"type": "fido2", "hint": "alice", "tenant_id": "t1",
		})
		require.NoError(t, err)
		assert.Equal(t, "s2", resp.(*qrdbtransport.BeginLoginResponse).SessionID)
	})

	t.Run("error_propagated", func(t *testing.T) {
		login := &fakeLoginServicer{
			beginFunc: func(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
				return nil, &LoginServiceUnavailableError{}
			},
		}
		_, err := executeLoginBegin(context.Background(), login, map[string]interface{}{"type": "mtls"})
		require.Error(t, err)
	})
}

// =============================================================================
// executeLoginFinish
// =============================================================================

func TestExecuteLoginFinish_DecodesFieldsAndError(t *testing.T) {
	t.Run("decodes_base64_and_metadata", func(t *testing.T) {
		sig := base64.StdEncoding.EncodeToString([]byte("proof"))
		lk := base64.StdEncoding.EncodeToString([]byte("key"))
		alt := base64.StdEncoding.EncodeToString([]byte("alt-key"))
		login := &fakeLoginServicer{
			finishFunc: func(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*service.FinishLoginResult, error) {
				assert.Equal(t, "sess", req.SessionID)
				assert.Equal(t, qrdbtransport.FactorType("piv"), req.Type)
				assert.Equal(t, []byte("proof"), req.Signature)
				assert.Equal(t, []byte("key"), req.LookupKey)
				assert.Equal(t, [][]byte{[]byte("alt-key")}, req.AltLookupKeys)
				assert.Equal(t, "9a", req.MethodMetadata["slot"])
				return &service.FinishLoginResult{FinishLoginResponse: &qrdbtransport.FinishLoginResponse{Bearer: "b1"}}, nil
			},
		}
		resp, err := executeLoginFinish(context.Background(), login, map[string]interface{}{
			"session_id":      "sess",
			"type":            "piv",
			"signature":       sig,
			"lookup_key":      lk,
			"alt_lookup_keys": []interface{}{alt},
			"method_metadata": map[string]interface{}{"slot": "9a", "ignored": 7},
		})
		require.NoError(t, err)
		assert.Equal(t, "b1", resp.(*service.FinishLoginResult).Bearer)
	})

	t.Run("error_propagated", func(t *testing.T) {
		login := &fakeLoginServicer{
			finishFunc: func(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*service.FinishLoginResult, error) {
				return nil, &LoginServiceUnavailableError{}
			},
		}
		_, err := executeLoginFinish(context.Background(), login, map[string]interface{}{"session_id": "s", "type": "mtls"})
		require.Error(t, err)
	})
}

// =============================================================================
// decodeLoginBase64
// =============================================================================

func TestDecodeLoginBase64(t *testing.T) {
	t.Run("absent_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginBase64(map[string]interface{}{}, "sig"))
	})
	t.Run("invalid_base64_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginBase64(map[string]interface{}{"sig": "!!!not-base64!!!"}, "sig"))
	})
	t.Run("valid_decodes", func(t *testing.T) {
		enc := base64.StdEncoding.EncodeToString([]byte("hello"))
		assert.Equal(t, []byte("hello"), decodeLoginBase64(map[string]interface{}{"sig": enc}, "sig"))
	})
}

// =============================================================================
// decodeLoginBase64Array
// =============================================================================

func TestDecodeLoginBase64Array(t *testing.T) {
	t.Run("absent_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginBase64Array(map[string]interface{}{}, "alt"))
	})
	t.Run("wrong_type_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginBase64Array(map[string]interface{}{"alt": "string"}, "alt"))
	})
	t.Run("empty_array_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginBase64Array(map[string]interface{}{"alt": []interface{}{}}, "alt"))
	})
	t.Run("all_invalid_entries_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginBase64Array(map[string]interface{}{
			"alt": []interface{}{"!!!not-base64!!!", 7, ""},
		}, "alt"))
	})
	t.Run("decodes_in_order_skipping_invalid", func(t *testing.T) {
		k1 := base64.StdEncoding.EncodeToString([]byte("first"))
		k2 := base64.StdEncoding.EncodeToString([]byte("second"))
		out := decodeLoginBase64Array(map[string]interface{}{
			"alt": []interface{}{k1, "!!!bad!!!", k2},
		}, "alt")
		require.Len(t, out, 2)
		assert.Equal(t, []byte("first"), out[0])
		assert.Equal(t, []byte("second"), out[1])
	})
}

// =============================================================================
// decodeLoginStringMap
// =============================================================================

func TestDecodeLoginStringMap(t *testing.T) {
	t.Run("missing_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginStringMap(map[string]interface{}{}, "md"))
	})
	t.Run("wrong_type_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginStringMap(map[string]interface{}{"md": "string"}, "md"))
	})
	t.Run("empty_map_returns_nil", func(t *testing.T) {
		assert.Nil(t, decodeLoginStringMap(map[string]interface{}{"md": map[string]interface{}{}}, "md"))
	})
	t.Run("filters_non_string_values", func(t *testing.T) {
		out := decodeLoginStringMap(map[string]interface{}{
			"md": map[string]interface{}{"a": "1", "b": 2, "c": "3"},
		}, "md")
		require.Len(t, out, 2)
		assert.Equal(t, "1", out["a"])
		assert.Equal(t, "3", out["c"])
		_, ok := out["b"]
		assert.False(t, ok)
	})
}
