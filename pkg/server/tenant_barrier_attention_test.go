//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// statusOnlyTenantBarrier answers TenantBarrierStatus and refuses every
// mutation: the attention reader only ever reads.
type statusOnlyTenantBarrier struct {
	service.TenantBarrierServicer
	status *qrdbtransport.TenantBarrierStatusResponse
	err    error
}

func (b *statusOnlyTenantBarrier) TenantBarrierStatus(_ context.Context, _ string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	return b.status, b.err
}

// TestTenantBarrierAttentionReader_CarriesInitialized is X3's adapter half:
// qrdb's Initialized flag reaches the inbox, which is what lets it tell a
// tenant with no barrier of its own from one whose barrier is sealed.
func TestTenantBarrierAttentionReader_CarriesInitialized(t *testing.T) {
	cases := []struct {
		name                string
		initialized, sealed bool
	}{
		{"no barrier of its own", false, true},
		{"initialized and sealed", true, true},
		{"initialized and unsealed", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := newTenantBarrierAttentionReader(&statusOnlyTenantBarrier{status: &qrdbtransport.TenantBarrierStatusResponse{
				TenantID: "acme", Initialized: tc.initialized, Sealed: tc.sealed,
			}})
			got, err := reader.BarrierStatus(context.Background(), "acme")
			require.NoError(t, err)
			require.Equal(t, "acme", got.Scope)
			require.Equal(t, tc.initialized, got.Initialized)
			require.Equal(t, tc.sealed, got.Sealed)
		})
	}
}

// TestTenantBarrierAttentionReader_SurfacesTheReadError is the error side: a
// status the node cannot give is an error, never a guessed state.
func TestTenantBarrierAttentionReader_SurfacesTheReadError(t *testing.T) {
	want := errors.New("tenant service unavailable")
	reader := newTenantBarrierAttentionReader(&statusOnlyTenantBarrier{err: want})
	_, err := reader.BarrierStatus(context.Background(), "acme")
	require.ErrorIs(t, err, want)
}
