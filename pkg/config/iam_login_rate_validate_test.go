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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidator_LoginRatePerMin_NegativeRefused pins that a negative login
// rate is refused at boot rather than handed to the xkms login module.
func TestValidator_LoginRatePerMin_NegativeRefused(t *testing.T) {
	v := NewValidator()
	v.validateIAM(&IAMConfig{LoginRatePerMin: -1})
	err := v.Errors()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iam.login_rate_per_min")
}

// TestValidator_LoginRatePerMin_ZeroAndPositivePass pins that zero (the
// module default) and a positive rate are accepted.
func TestValidator_LoginRatePerMin_ZeroAndPositivePass(t *testing.T) {
	for _, rate := range []int{0, 30} {
		t.Run(strconv.Itoa(rate), func(t *testing.T) {
			v := NewValidator()
			v.validateIAM(&IAMConfig{LoginRatePerMin: rate})
			assert.NoError(t, v.Errors())
		})
	}
}

// TestValidator_LoginRatePerMin_LoadsFromYAML pins that the key reaches the
// config from a file.
func TestValidator_LoginRatePerMin_LoadsFromYAML(t *testing.T) {
	cfg := loadSelfRegYAML(t, "iam:\n  login_rate_per_min: 25\n")
	require.Equal(t, 25, cfg.IAM.LoginRatePerMin)
}
