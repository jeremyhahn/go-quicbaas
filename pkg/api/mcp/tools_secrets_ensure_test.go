//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func newEnsureTestRegistry(t *testing.T) (*ToolRegistry, *testutil.MockQuicBaaS) {
	t.Helper()
	svc := &testutil.MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log), svc
}

// TestEnsureSecretMCP_HappyPath exercises the quicbaas_secrets_ensure
// tool end-to-end through the ToolRegistry.ExecuteTool dispatch.
func TestEnsureSecretMCP_HappyPath(t *testing.T) {
	r, svc := newEnsureTestRegistry(t)

	path := "tenants/t1/nodes/n1/secrets/platform/so-pin"
	svc.On("EnsureSecret", mock.Anything, mock.MatchedBy(func(req *service.EnsureSecretRequest) bool {
		return req.SecretID == path && req.Length == 6 && req.Encoding == secrets.EncodingNumeric
	})).Return(&service.EnsureSecretResponse{
		SecretID:  path,
		Data:      []byte("123456"),
		Generated: true,
		Version:   1,
		Timestamp: time.Now(),
	}, nil).Once()

	out, err := r.ExecuteTool(context.Background(), "quicbaas_secrets_ensure", map[string]interface{}{
		"path":     path,
		"length":   float64(6), // JSON numbers arrive as float64
		"encoding": secrets.EncodingNumeric,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	svc.AssertExpectations(t)
}

// TestEnsureSecretMCP_InvalidSpec verifies that malformed input is
// rejected at the MCP boundary before any service call.
func TestEnsureSecretMCP_InvalidSpec(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"missing path", map[string]interface{}{"length": float64(8), "encoding": "hex"}},
		{"zero length", map[string]interface{}{"path": "p", "length": float64(0), "encoding": "hex"}},
		{"unknown encoding", map[string]interface{}{"path": "p", "length": float64(8), "encoding": "bogus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, svc := newEnsureTestRegistry(t)
			_, err := r.ExecuteTool(context.Background(), "quicbaas_secrets_ensure", tc.args)
			assert.Error(t, err)
			svc.AssertNotCalled(t, "EnsureSecret", mock.Anything, mock.Anything)
		})
	}
}

// TestEnsureSecretMCP_SpecMismatchPropagates verifies that service-
// layer spec-mismatch errors are surfaced, not swallowed.
func TestEnsureSecretMCP_SpecMismatchPropagates(t *testing.T) {
	r, svc := newEnsureTestRegistry(t)
	svc.On("EnsureSecret", mock.Anything, mock.Anything).
		Return((*service.EnsureSecretResponse)(nil), secrets.ErrSecretSpecMismatch).Once()

	_, err := r.ExecuteTool(context.Background(), "quicbaas_secrets_ensure", map[string]interface{}{
		"path":     "p",
		"length":   float64(16),
		"encoding": secrets.EncodingBase64,
	})
	require.Error(t, err)
	svc.AssertExpectations(t)
}
