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

package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSecretRequest_Payload(t *testing.T) {
	raw, err := (&StoreSecretRequest{Raw: []byte{0x00, 0xff}}).Payload()
	require.NoError(t, err)
	assert.Equal(t, []byte{0x00, 0xff}, raw, "Raw is returned verbatim")

	encoded, err := (&StoreSecretRequest{Data: map[string]interface{}{"k": "v"}}).Payload()
	require.NoError(t, err)
	assert.JSONEq(t, `{"k":"v"}`, string(encoded))
}

func TestStoreSecretRequest_PayloadUnencodable(t *testing.T) {
	_, err := (&StoreSecretRequest{Data: map[string]interface{}{"k": make(chan int)}}).Payload()
	assert.Error(t, err)
}

func TestValidateStoreSecretRequest_RawAndData(t *testing.T) {
	assert.NoError(t, validateStoreSecretRequest(&StoreSecretRequest{Path: "p", Raw: []byte("x")}))
	assert.Error(t, validateStoreSecretRequest(&StoreSecretRequest{Path: "p"}), "neither Data nor Raw")
	assert.Error(t, validateStoreSecretRequest(&StoreSecretRequest{
		Path: "p", Raw: []byte("x"), Data: map[string]interface{}{"k": "v"},
	}), "both Data and Raw")
}
