//go:build mcp && !integration

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

package server

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestServerStart_ConflictingBindAddressesRefused: Start never runs config
// validation, so the embedded node's configuration is what refuses listeners
// that disagree on their bind host. The boot fails before any listener binds,
// instead of putting MCP kept on loopback onto every interface.
func TestServerStart_ConflictingBindAddressesRefused(t *testing.T) {
	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	cfg := productionPostureConfig(t, false)
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.REST.BindAddress = ""
	cfg.Server.GRPC.BindAddress = ""
	cfg.Server.MCP.BindAddress = "127.0.0.1"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startErr := srv.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	require.Error(t, startErr, "conflicting bind addresses must refuse the boot")
	var nodeErr *QRDBNodeError
	require.True(t, errors.As(startErr, &nodeErr), "want *QRDBNodeError, got %T: %v", startErr, startErr)
	assert.Equal(t, "build qrdb config", nodeErr.Stage)
	var verr *config.ValidationError
	require.True(t, errors.As(startErr, &verr), "want *config.ValidationError, got %T: %v", startErr, startErr)
	assert.Equal(t, "server.mcp.bind_address", verr.Field)
	assert.Nil(t, srv.qrdbNode, "no embedded node may start")

	for name, port := range map[string]int{"mcp": cfg.Server.MCP.Port, "rest": cfg.Server.REST.Port} {
		conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if dialErr == nil {
			_ = conn.Close()
		}
		assert.Error(t, dialErr, "nothing may listen on the %s port", name)
	}
}
