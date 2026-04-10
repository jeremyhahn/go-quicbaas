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

// Package main provides tests for seal init response with recovery keys
// to cover the len(resp.RecoveryKeys) > 0 branch in formatSealInitResponse
// in both the table and default format cases.
package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestFormatSealInitResponse_TableWithRecoveryKeys(t *testing.T) {
	resp := &service.SealInitResponse{
		Shares:       [][]byte{[]byte("share1"), []byte("share2")},
		RecoveryKeys: [][]byte{[]byte("recovery1"), []byte("recovery2")},
		RootToken:    "root-token",
		Version:      1,
		Timestamp:    time.Now(),
	}

	var buf bytes.Buffer
	err := formatSealInitResponse(resp, "table", &buf)
	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "SEAL INITIALIZATION COMPLETE")
	assert.Contains(t, output, "Recovery Keys")
}

func TestFormatSealInitResponse_DefaultWithRecoveryKeys(t *testing.T) {
	resp := &service.SealInitResponse{
		Shares:       [][]byte{[]byte("share1"), []byte("share2")},
		RecoveryKeys: [][]byte{[]byte("recovery1"), []byte("recovery2")},
		RootToken:    "root-token",
		Version:      1,
		Timestamp:    time.Now(),
	}

	var buf bytes.Buffer
	err := formatSealInitResponse(resp, "raw", &buf)
	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "SEAL INITIALIZED")
	assert.Contains(t, output, "Recovery Keys")
}
