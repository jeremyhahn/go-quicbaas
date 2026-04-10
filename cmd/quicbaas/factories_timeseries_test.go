//go:build timeseries

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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateTimeSeriesRESTClient(t *testing.T) {
	client, err := createTimeSeriesRESTClient()
	assert.Nil(t, client)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REST transport is not yet implemented")
}

// TestCreateTimeSeriesClient_UnsupportedTransport verifies unsupported transport error.
func TestCreateTimeSeriesClient_UnsupportedTransport(t *testing.T) {
	// Save and restore the global transport variable
	originalTransport := transport
	defer func() { transport = originalTransport }()

	transport = "unsupported_protocol"
	client, err := createTimeSeriesClient()
	assert.Nil(t, client)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

// TestCreateTimeSeriesClient_QUICTransport verifies QUIC transport returns not-supported.
func TestCreateTimeSeriesClient_QUICTransport(t *testing.T) {
	originalTransport := transport
	defer func() { transport = originalTransport }()

	transport = "quic"
	client, err := createTimeSeriesClient()
	assert.Nil(t, client)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC transport is not yet supported")
}
