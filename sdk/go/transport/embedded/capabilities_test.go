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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	servercaps "github.com/jeremyhahn/go-quicbaas/pkg/capabilities"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/capabilities"
)

// TestTransport_CapabilitiesGet reads the report in process through the SDK
// client and checks it is the one capabilities.Build makes from the service.
func TestTransport_CapabilitiesGet(t *testing.T) {
	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	report, err := capabilities.NewClient(tr).Get(context.Background())
	require.NoError(t, err)

	want := servercaps.Build(mock, servercaps.Probes{TimeSeriesServed: true})
	require.Len(t, report.Subsystems, len(want.Subsystems))
	for name, sub := range want.Subsystems {
		got := report.Subsystems[name]
		assert.Equal(t, sub.Served, got.Served, name)
		assert.Equal(t, sub.Configured, got.Configured, name)
		assert.Equal(t, sub.Reason, got.Reason, name)
	}
	assert.Equal(t, want.Families, report.Families)

	// The embedded transport runs no transport server, so it has no CA probe.
	xkms := report.Subsystems[servercaps.SubsystemXKMSCA]
	assert.False(t, xkms.Configured)
	assert.NotEmpty(t, xkms.Reason)

	// The embedded transport always compiles its time series handlers; the
	// mock facade wires no time series storage.
	ts := report.Subsystems[servercaps.SubsystemTimeSeries]
	assert.True(t, ts.Served)
	assert.False(t, ts.Configured)
	assert.Equal(t, servercaps.ReasonNoTimeSeriesStorage, ts.Reason)

	// The embedded transport has no RAG handlers.
	assert.NotContains(t, report.Families, servercaps.FamilyRAG)
}
