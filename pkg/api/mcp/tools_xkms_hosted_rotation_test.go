//go:build mcp

// Copyright (c) 2025 Jeremy Hahn

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// xkms.rotateKey on the platform OIDC signing backend is refused with the
// typed error, which the tool answers as permission denied.
func TestHandleXKMSRotateKey_OIDCSigningBackendUnavailableWithoutDelegate(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	registry := NewToolRegistry(nil, log)

	_, err = registry.handleXKMSRotateKey(context.Background(), map[string]interface{}{
		"key_id":  "oidc-signing",
		"backend": dbxkms.OIDCSigningBackendName,
	})
	var unavailable *dbxkms.RotationDelegateUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, mcptransport.MCPServiceUnavailable, toolErrorCode(err))
}
