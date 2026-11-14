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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad_RefusesPerProtocolBindAddress proves the per-protocol bind
// overrides are not silently ignored. The embedded qrdb node binds every API
// listener (REST, gRPC, MCP) on server.host, so a server.<protocol>.bind_address
// never reached a listener: an operator who set one for private ingress got
// the shared host instead. Loading such a configuration is refused by name.
func TestLoad_RefusesPerProtocolBindAddress(t *testing.T) {
	for _, key := range []string{
		"server.rest.bind_address",
		"server.grpc.bind_address",
		"server.mcp.bind_address",
		"server.unix.bind_address",
	} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quicbaas.yaml")
			require.NoError(t, os.WriteFile(path, []byte(nestedYAML(key, "10.0.0.4")), 0o600))

			_, err := NewLoader().Load(path)
			require.Error(t, err, "a configuration setting %s must be refused", key)
			var removed *RemovedKeyError
			require.True(t, errors.As(err, &removed), "%v", err)
			assert.Equal(t, key, removed.Key)
			assert.Contains(t, removed.Replacement, "server.host")
			assert.True(t, errors.Is(err, ErrRemovedKey))
		})
	}
}
