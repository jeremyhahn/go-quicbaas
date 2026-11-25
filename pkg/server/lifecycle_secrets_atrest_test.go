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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

func newAtRestTestServer(t *testing.T, atRest bool) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return &Server{config: &config.Config{}, logger: log, secretsAtRest: atRest}
}

func TestStartSecrets_SealOffManagerRefusesWrites(t *testing.T) {
	s := newAtRestTestServer(t, false)
	require.NoError(t, s.startSecrets())
	require.NotNil(t, s.secretsManager)

	err := s.secretsManager.Store(context.Background(), "a/b", map[string][]byte{"v": []byte("x")}, nil)
	assert.True(t, errors.Is(err, secrets.ErrAtRestEncryptionRequired), "got %v", err)
}

func TestStartSecrets_SealOnManagerStores(t *testing.T) {
	s := newAtRestTestServer(t, true)
	require.NoError(t, s.startSecrets())
	require.NotNil(t, s.secretsManager)

	require.NoError(t, s.secretsManager.Store(context.Background(), "a/b", map[string][]byte{"v": []byte("x")}, nil))
}

func TestBuildQRDBConfig_AtRestFollowsSeal(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		strategy string
		want     bool
	}{
		{name: "seal off", enabled: false, strategy: "passphrase", want: false},
		{name: "seal on with passphrase", enabled: true, strategy: "passphrase", want: true},
		{name: "seal on with strategy none", enabled: true, strategy: "none", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Storage.Backend = config.StorageBackendMemory
			cfg.Seal.Enabled = tt.enabled
			cfg.Seal.Strategy = tt.strategy

			qcfg := mustBuildQRDBConfig(t, cfg, false)

			assert.Equal(t, tt.want, platformAtRest(qcfg))
		})
	}
}
