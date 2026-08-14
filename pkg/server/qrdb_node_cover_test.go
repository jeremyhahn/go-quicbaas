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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBuildXKMSCryptoOptions_SoftwareDefault covers the software/default path of
// buildXKMSCryptoOptions: with the default (passphrase/software) strategy no
// hardware factory is selected, the nil-logger branch installs slog.Default,
// the PIN backend is built, and the crypto options are returned without error.
func TestBuildXKMSCryptoOptions_SoftwareDefault(t *testing.T) {
	cfg := config.TestConfig()

	opts, _, err := buildXKMSCryptoOptions(cfg, nil, nil, nil)
	require.NoError(t, err, "software-strategy crypto options must build")
	assert.NotNil(t, opts)
}

// TestBuildXKMSCryptoOptions_HardwareStrategyFactory covers the hardware-strategy
// branch: with a registered hardware strategy (tpm2) the factory closure is
// captured into StrategyFactories. The closure is not invoked here (that would
// require a TPM), so the build stays hermetic.
func TestBuildXKMSCryptoOptions_HardwareStrategyFactory(t *testing.T) {
	cfg := config.TestConfig()
	cfg.Seal.Strategy = "tpm2"

	opts, _, err := buildXKMSCryptoOptions(cfg, nil, nil, nil)
	require.NoError(t, err, "the tpm2 factory closure must be captured without invoking the TPM")
	assert.NotNil(t, opts)
}

// TestBuildXKMSFactors covers the auth-factor plugin construction for both the
// RPID-set and nil-config paths.
func TestBuildXKMSFactors(t *testing.T) {
	cfg := config.TestConfig()
	cfg.IAM.WebAuthn.RPID = "example.test"
	withRPID := buildXKMSFactors(cfg)
	assert.Len(t, withRPID, 4, "PIV + PKCS#11 + FIDO2 + mTLS plugins must be registered")

	nilCfg := buildXKMSFactors(nil)
	assert.Len(t, nilCfg, 4, "a nil config must still yield the default plugins")
}
