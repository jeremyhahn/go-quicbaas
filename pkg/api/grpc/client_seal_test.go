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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func TestClientSealInit_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SealInit(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientUnseal_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.Unseal(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientRekey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.Rekey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestConvertProtoSealModeManual(t *testing.T) {
	result := convertProtoSealMode(pb.SealMode_SEAL_MODE_MANUAL)
	assert.Equal(t, "manual", result)
}

func TestConvertProtoSealModeAuto(t *testing.T) {
	result := convertProtoSealMode(pb.SealMode_SEAL_MODE_AUTO)
	assert.Equal(t, "auto", result)
}

func TestConvertProtoSealModeCrypto(t *testing.T) {
	result := convertProtoSealMode(pb.SealMode_SEAL_MODE_CRYPTO)
	assert.Equal(t, "crypto", result)
}

func TestConvertProtoSealModeUnspecified(t *testing.T) {
	result := convertProtoSealMode(pb.SealMode_SEAL_MODE_UNSPECIFIED)
	assert.Equal(t, "unspecified", result)
}

func TestConvertProtoSealModeUnknownValue(t *testing.T) {
	result := convertProtoSealMode(pb.SealMode(999))
	assert.Equal(t, "unspecified", result)
}
