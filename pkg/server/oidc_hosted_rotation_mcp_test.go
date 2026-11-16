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
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Two nodes share one replicated store. An MCP hosted rotation of the
// platform OIDC key on node B returns the typed refusal and leaves the shared
// version record and key material as they were; SigningKeyManager.Rotate
// still rotates it.
func TestOIDCSigningStore_MCPHostedRotationUnavailableWithoutDelegate(t *testing.T) {
	a, b, kv, before, _ := hostedRotationNodes(t)

	_, err := mcpHostedRotate(t, tenantctx.WithSystem(context.Background()))
	var unavailable *dbxkms.RotationDelegateUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, before, snapshotKV(t, kv))

	requireManagerRotates(t, a, b)
}

// mcpHostedRotate runs the xkms.rotateKey MCP tool on the OIDC signing key
// as a caller whose context is ctx.
func mcpHostedRotate(t *testing.T, ctx context.Context) (interface{}, error) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return mcp.NewToolRegistry(nil, log).ExecuteTool(ctx, "xkms.rotateKey", map[string]interface{}{
		"key_id":  iam.OIDCSigningKeyCN,
		"backend": oidcSigningBackendName,
	})
}

// viaMCP rotates through the xkms.rotateKey MCP tool, which names the OIDC
// signing key by backend and key name.
var viaMCP = hostedRotate{
	rotate: func(t *testing.T, ctx context.Context, _ string) error {
		_, err := mcpHostedRotate(t, ctx)
		return err
	},
	typed: true,
}

func TestOIDCSigningStore_MCPHostedRotationRotatesThroughLease(t *testing.T) {
	requireHostedRotationRotatesThroughLease(t, viaMCP)
}

func TestOIDCSigningStore_MCPConcurrentHostedRotationLosesLease(t *testing.T) {
	requireConcurrentHostedRotationLosesLease(t, viaMCP)
}

func TestOIDCSigningStore_MCPUnauthorizedHostedRotationRefused(t *testing.T) {
	requireUnauthorizedHostedRotationRefused(t, viaMCP)
}

func TestOIDCSigningStore_MCPDedicatedTenantHostedRotation(t *testing.T) {
	requireDedicatedTenantKeyRotates(t, viaMCP)
}
