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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoaderLoad_SPIFFETrustDomainFromEnv proves that a deployment can supply
// iam.spiffe_trust_domain through QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN when the
// config file does not name the key, and that nothing defaults it.
func TestLoaderLoad_SPIFFETrustDomainFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{name: "from the environment", env: "quicbaas.example.test"},
		{name: "unset stays empty", env: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quicbaas.yaml")
			require.NoError(t, os.WriteFile(path, []byte("logging:\n  level: warn\n"), 0o600))
			t.Setenv("QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN", tt.env)

			cfg, err := NewLoader().Load(path)
			require.NoError(t, err)
			require.Equal(t, tt.env, cfg.IAM.SPIFFETrustDomain)
		})
	}
}
