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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIAMLoginRatePerMin_LoadsFromYAML(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  login_rate_per_min: 20
`)
	assert.Equal(t, 20, cfg.IAM.LoginRatePerMin)

	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	assert.NoError(t, v.Errors())
}

func TestIAMLoginRatePerMin_UnsetIsZero(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  spiffe_trust_domain: quicbaas.example.test
`)
	assert.Zero(t, cfg.IAM.LoginRatePerMin, "unset must leave go-xkms's default in force")

	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	assert.NoError(t, v.Errors())
}

func TestIAMLoginRatePerMin_NegativeRefused(t *testing.T) {
	v := NewValidator()
	v.validateIAM(&IAMConfig{LoginRatePerMin: -1})

	err := v.Errors()
	require.Error(t, err)
	var rateErr *LoginRatePerMinError
	require.True(t, errors.As(err, &rateErr), "expected *LoginRatePerMinError, got: %v", err)
	assert.Equal(t, -1, rateErr.Value)
	assert.Contains(t, err.Error(), "iam.login_rate_per_min")
}
