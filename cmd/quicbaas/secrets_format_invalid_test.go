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

// Package main provides tests for secrets format functions with invalid
// output format to cover the ParseOutputFormat error paths.
package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestFormatSecretsRotateResponse_InvalidFormat(t *testing.T) {
	resp := &service.RotateSecretResponse{
		SecretID:   "test/secret",
		OldVersion: 1,
		NewVersion: 2,
		Timestamp:  time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsRotateResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsGetResponse_InvalidFormat(t *testing.T) {
	resp := &service.GetSecretResponse{
		SecretID:  "test/secret",
		Data:      []byte("secret-data"),
		Version:   1,
		CreatedAt: time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsGetResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsListResponse_InvalidFormat(t *testing.T) {
	resp := &service.ListSecretsResponse{
		Secrets: []*service.Secret{},
	}
	var buf bytes.Buffer
	err := formatSecretsListResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsDeleteResponse_InvalidFormat(t *testing.T) {
	resp := &service.DeleteSecretResponse{
		SecretID:  "test/secret",
		Deleted:   true,
		Timestamp: time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsDeleteResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsVersionsResponse_InvalidFormat(t *testing.T) {
	resp := &service.ListSecretVersionsResponse{
		Versions:  []*service.SecretVersion{},
		Timestamp: time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsVersionsResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsRollbackResponse_InvalidFormat(t *testing.T) {
	resp := &service.RollbackSecretResponse{
		SecretID:   "test/secret",
		NewVersion: 1,
		Timestamp:  time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsRollbackResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsStatusResponse_InvalidFormat(t *testing.T) {
	resp := &service.UpdateSecretStatusResponse{
		SecretID:  "test/secret",
		Timestamp: time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsStatusResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsGetPrefixResponse_InvalidFormat(t *testing.T) {
	resp := &service.GetSecretsByPrefixResponse{
		Secrets: map[string]map[string][]byte{},
	}
	var buf bytes.Buffer
	err := formatSecretsGetPrefixResponse(resp, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}

func TestFormatSecretsWatchChange_InvalidFormat(t *testing.T) {
	change := &service.SecretChange{
		Path:      "test/secret",
		Timestamp: time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsWatchChange(change, "invalid_xyz", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_xyz")
}
