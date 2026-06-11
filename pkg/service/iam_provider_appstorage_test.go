// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIAMServiceAdapter_GetAppStorage_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	as, err := adapter.GetAppStorage(context.Background())
	require.NoError(t, err)
	assert.Nil(t, as)
}

func TestIAMServiceAdapter_GetAppStorage_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	as, err := adapter.GetAppStorage(context.Background())
	require.NoError(t, err)
	assert.Nil(t, as)
}
