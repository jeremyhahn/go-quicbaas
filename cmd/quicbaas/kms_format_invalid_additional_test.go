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

// Package main provides tests for KMS format functions with invalid output
// format to cover the ParseOutputFormat error paths in formatKMSKeyGenerateResponse,
// formatKMSEncryptResponse, and formatKMSDecryptResponse.
package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestFormatKMSKeyGenerateResponse_InvalidFormat(t *testing.T) {
	resp := &service.GenerateKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     "gen-key-1",
			Algorithm: service.KeyAlgAES256GCM,
			Version:   1,
			Status:    service.KeyStatusEnabled,
			CreatedAt: time.Now(),
		},
	}
	var buf bytes.Buffer
	err := formatKMSKeyGenerateResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatKMSEncryptResponse_InvalidFormat(t *testing.T) {
	resp := &service.EncryptResponse{
		KeyID:      "enc-key-1",
		Ciphertext: []byte("encrypted-data"),
	}
	var buf bytes.Buffer
	err := formatKMSEncryptResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatKMSDecryptResponse_InvalidFormat(t *testing.T) {
	resp := &service.DecryptResponse{
		Plaintext: []byte("decrypted-data"),
	}
	var buf bytes.Buffer
	err := formatKMSDecryptResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}
