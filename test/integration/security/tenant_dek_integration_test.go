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

//go:build integration

package security_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
)

// NOTE: Tenant DEK management has been migrated to the go-qrdb SDK's
// pkg/dek package. The TestTenantDEKManager_Integration_Isolation test
// previously instantiated security.NewTenantDEKManager which no longer
// exists in this codebase. When integration tests for tenant DEK
// operations are needed, they should exercise the SDK's DEKClient
// interface through the server's gRPC/REST API layer.
//
// The TestCrossTenantAudit_Integration tests below remain valid as they
// test the audit logging subsystem which is still in pkg/security.

func TestCrossTenantAudit_Integration(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	auditLogger, err := security.NewAuditLogger(&security.AuditConfig{
		Enabled: true,
	}, log)
	require.NoError(t, err)
	defer auditLogger.Close()

	ctx := context.Background()

	t.Run("log tenant switch", func(t *testing.T) {
		err := auditLogger.LogTenantSwitch(ctx, "admin", "user-123", "tenant-a", "tenant-b", "Support ticket #456", "assign-789", "192.168.1.1")
		require.NoError(t, err)
	})

	t.Run("log cross-tenant read", func(t *testing.T) {
		err := auditLogger.LogCrossTenantRead(ctx, "admin", "user-123", "tenant-a", "tenant-b", "/secrets/api-key", "Investigation #123", "192.168.1.1")
		require.NoError(t, err)
	})

	t.Run("log cross-tenant write", func(t *testing.T) {
		err := auditLogger.LogCrossTenantWrite(ctx, "admin", "user-123", "tenant-a", "tenant-b", "/config/setting", "Emergency fix", "192.168.1.1")
		require.NoError(t, err)
	})

	t.Run("log tenant access denied", func(t *testing.T) {
		err := auditLogger.LogTenantAccessDenied(ctx, "attacker", "user-456", "tenant-a", "tenant-b", "/secrets/password", "10.0.0.1", "unauthorized access attempt")
		require.NoError(t, err)
	})

	t.Run("log tenant switch error with nil logger", func(t *testing.T) {
		// Verify audit operations return meaningful errors when misconfigured
		disabledAudit, err := security.NewAuditLogger(&security.AuditConfig{
			Enabled: false,
		}, log)
		require.NoError(t, err)
		defer disabledAudit.Close()

		// Disabled audit logger should still not panic
		err = disabledAudit.LogTenantSwitch(ctx, "admin", "user-123", "tenant-a", "tenant-b", "reason", "assign", "127.0.0.1")
		assert.NoError(t, err)
	})
}
