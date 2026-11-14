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

// countKMSClientBuilds points kmsClientFactory at client and returns a
// pointer to the number of times a command built it (#2111).
func countKMSClientBuilds(t *testing.T, client KMSClient) *int {
	t.Helper()
	builds := 0
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		builds++
		return client, nil
	}
	t.Cleanup(func() { kmsClientFactory = origFactory })
	return &builds
}

func TestKMSKeyRotate_GracePeriodReachesRequest(t *testing.T) {
	var sent *service.RotateKeyRequest
	countKMSClientBuilds(t, &MockKMSClient{
		KMSRotateKeyFunc: func(_ context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
			sent = req
			return &service.RotateKeyResponse{KeyID: req.KeyID}, nil
		},
	})

	cmd := newKMSKeyRotateCommand()
	cmd.SetArgs([]string{"rotated-key", "--grace-period", "7d"})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, sent)
	assert.Equal(t, int64(7*24*60*60), sent.GracePeriodSeconds)
}

func TestKMSKeyRotate_NoGracePeriodSendsZero(t *testing.T) {
	var sent *service.RotateKeyRequest
	countKMSClientBuilds(t, &MockKMSClient{
		KMSRotateKeyFunc: func(_ context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
			sent = req
			return &service.RotateKeyResponse{KeyID: req.KeyID}, nil
		},
	})

	cmd := newKMSKeyRotateCommand()
	cmd.SetArgs([]string{"rotated-key"})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, sent)
	assert.Zero(t, sent.GracePeriodSeconds)
}

func TestKMSKeyRotate_InvalidGracePeriodRefusedBeforeClient(t *testing.T) {
	builds := countKMSClientBuilds(t, &MockKMSClient{})

	cmd := newKMSKeyRotateCommand()
	cmd.SetArgs([]string{"rotated-key", "--grace-period", "soon"})
	require.Error(t, cmd.Execute())

	assert.Zero(t, *builds, "an invalid --grace-period must be refused before the client is built")
}

func TestKMSKeyImport_InvalidMaterialRefusedBeforeClient(t *testing.T) {
	builds := countKMSClientBuilds(t, &MockKMSClient{})

	cmd := newKMSKeyImportCommand()
	cmd.SetArgs([]string{"imported-key", "--material", "base64:%%%not-base64%%%"})
	require.Error(t, cmd.Execute())

	assert.Zero(t, *builds, "invalid key material must be refused before the client is built")
}
