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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientStoreSecret_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.StoreSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientGetSecret_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.GetSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientListSecrets_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.ListSecrets(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientDeleteSecret_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.DeleteSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientListSecretVersions_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.ListSecretVersions(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientRollbackSecret_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.RollbackSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientRotateSecret_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.RotateSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientUpdateSecretStatus_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.UpdateSecretStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientGetSecretsByPrefix_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.GetSecretsByPrefix(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientWatchSecretsByPrefix_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	ch, err := client.WatchSecretsByPrefix(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "request cannot be nil")
}
