//go:build !integration

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

// crypto_options_errors_test.go extends crypto_options_test.go with the
// strategy registration error path. The default registry is pre-populated
// with Software + None strategies; re-registering one of those names
// through XKMSCryptoOptions exercises the strategyReg.Register error branch.

package server

import (
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXKMSCryptoOptions_DuplicateStrategyIsError(t *testing.T) {
	// Software is part of the default registry; passing it again through
	// StrategyFactories must surface an XKMSCryptoError with the canonical
	// "register strategy <name>" stage marker.
	cfg := &XKMSCryptoConfig{
		StrategyFactories: map[qrdbsdk.StrategyID]qrdbsdk.StrategyFactory{
			qrdbsdk.StrategyPassphrase: func() (qrdbsdk.SealingStrategy, error) {
				return qrdbsdk.NewNoneStrategy(), nil
			},
		},
	}
	_, err := XKMSCryptoOptions(cfg)
	require.Error(t, err)
	var ce *XKMSCryptoError
	require.True(t, errors.As(err, &ce))
	assert.Contains(t, ce.Stage, "register strategy")
	assert.Contains(t, ce.Stage, string(qrdbsdk.StrategyPassphrase))
}

func TestXKMSCryptoError_NoCause(t *testing.T) {
	e := &XKMSCryptoError{Stage: "stage-x"}
	got := e.Error()
	assert.Contains(t, got, "xkms crypto options failed at")
	assert.Contains(t, got, "stage-x")
	// No cause means no trailing ": <cause>" suffix beyond the stage name.
	assert.NotContains(t, got, "stage-x: ")
	assert.Nil(t, e.Unwrap())
}
