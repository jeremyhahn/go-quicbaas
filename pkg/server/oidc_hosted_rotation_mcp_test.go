//go:build mcp

// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Two nodes share one replicated store. An MCP hosted rotation of the
// platform OIDC key on node B returns the typed refusal and leaves the shared
// version record and key material as they were; SigningKeyManager.Rotate
// still rotates it.
func TestOIDCSigningStore_MCPHostedRotationRefusedOnPeer(t *testing.T) {
	a, b, kv, before, _ := hostedRotationNodes(t)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	_, err = mcp.NewToolRegistry(nil, log).ExecuteTool(context.Background(), "xkms.rotateKey", map[string]interface{}{
		"key_id":  iam.OIDCSigningKeyCN,
		"backend": oidcSigningBackendName,
	})
	var refused *dbxkms.OIDCSigningKeyRotationRefusedError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, before, snapshotKV(t, kv))

	requireManagerRotates(t, a, b)
}
