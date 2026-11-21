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

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// useKMSClient points kmsClientFactory at client for the rest of the test.
func useKMSClient(t *testing.T, client KMSClient) {
	t.Helper()
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return client, nil }
	t.Cleanup(func() { kmsClientFactory = origFactory })
}

// importRequestFromCLI runs `kms key import` with args and returns the request
// the command sent.
func importRequestFromCLI(t *testing.T, args ...string) *service.ImportKeyRequest {
	t.Helper()
	var sent *service.ImportKeyRequest
	useKMSClient(t, &MockKMSClient{
		KMSImportKeyFunc: func(_ context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
			sent = req
			return &service.ImportKeyResponse{Key: &service.KeyHandle{KeyID: req.Name}}, nil
		},
	})

	cmd := newKMSKeyImportCommand()
	cmd.SetArgs(append([]string{"imported-key", "--material", "base64:SGVsbG8gd29ybGQgc2VjcmV0IGtleSE="}, args...))
	require.NoError(t, cmd.Execute())
	require.NotNil(t, sent)
	require.NotNil(t, sent.KeyAttributes)
	return sent
}

// unwrapRequestFromCLI runs `kms key unwrap` with args and returns the request
// the command sent.
func unwrapRequestFromCLI(t *testing.T, args ...string) *service.UnwrapKeyRequest {
	t.Helper()
	var sent *service.UnwrapKeyRequest
	useKMSClient(t, &MockKMSClient{
		KMSUnwrapKeyFunc: func(_ context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
			sent = req
			return &service.UnwrapKeyResponse{KeyID: req.KeyID}, nil
		},
	})

	cmd := newKMSKeyUnwrapCommand()
	cmd.SetArgs(append([]string{"unwrapped-key", "--wrapping-key", "wk-1", "--material", "SGVsbG8=", "--algorithm", "AES-256-GCM"}, args...))
	require.NoError(t, cmd.Execute())
	require.NotNil(t, sent)
	require.NotNil(t, sent.KeyAttributes)
	return sent
}

func TestKMSKeyImport_ExportableFlagReachesRequest(t *testing.T) {
	req := importRequestFromCLI(t, "--exportable")

	assert.True(t, req.KeyAttributes.Exportable)
}

func TestKMSKeyImport_NotExportableByDefault(t *testing.T) {
	req := importRequestFromCLI(t)

	assert.False(t, req.KeyAttributes.Exportable)
}

func TestKMSKeyUnwrap_ExportableFlagReachesRequest(t *testing.T) {
	req := unwrapRequestFromCLI(t, "--exportable")

	assert.True(t, req.KeyAttributes.Exportable)
}

func TestKMSKeyUnwrap_NotExportableByDefault(t *testing.T) {
	req := unwrapRequestFromCLI(t)

	assert.False(t, req.KeyAttributes.Exportable)
}

func TestKMSKeyUnwrap_ExportableFlagRejectsNonBoolean(t *testing.T) {
	useKMSClient(t, &MockKMSClient{})

	cmd := newKMSKeyUnwrapCommand()
	cmd.SetArgs([]string{"unwrapped-key", "--wrapping-key", "wk-1", "--material", "SGVsbG8=", "--algorithm", "AES-256-GCM", "--exportable=maybe"})
	assert.Error(t, cmd.Execute())
}
