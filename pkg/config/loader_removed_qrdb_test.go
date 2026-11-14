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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// go-qrdb under go-quicbaas is embedded only (#1617). The old qrdb: block,
// which offered network transports to a remote go-qrdb, is refused by name
// so a deployment that still carries it learns the setting is gone.
func TestLoad_RefusesTheQRDBBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "qrdb:\n  transport: grpc\n  address: qrdb.internal:50051\n  tls_enabled: true\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	_, err := NewLoader().Load(path)
	require.Error(t, err)
	var removed *RemovedKeyError
	require.True(t, errors.As(err, &removed), "%v", err)
	assert.Equal(t, "qrdb", removed.Key)
	assert.True(t, errors.Is(err, ErrRemovedKey))
	assert.Contains(t, err.Error(), "embedded only")
}

// A configuration without the qrdb: block loads.
func TestLoad_AcceptsAConfigWithoutTheQRDBBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "databases:\n  max_per_tenant: 7\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := NewLoader().Load(path)
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.Databases.MaxPerTenant)
}
