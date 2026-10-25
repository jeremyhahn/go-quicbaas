//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// buildBootstrapClientFixture assembles the seal-disabled wiring exactly as
// startService does: a memory node (no seal barrier), the bootstrap ceremony
// stack, and the in-process qrdb SDK client over that stack.
func buildBootstrapClientFixture(t *testing.T) *Server {
	t.Helper()

	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}

	bundle, err := buildBootstrapCeremonyServices(slog.Default(), t.TempDir(), testTrustDomain)
	require.NoError(t, err)

	client, err := newBootstrapCeremonyClient(
		s.GetQRDBKVServicer(), s.GetQRDBAdminServicer(), bundle, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, client)
	t.Cleanup(func() { _ = client.Close() })

	s.ceremonyServices = bundle
	s.bootstrapCeremonyClient = client
	return s
}

// newBootstrapCeremonyClient must reject a nil or incomplete ceremony
// services bundle with a typed CeremonyWiringError.
func TestNewBootstrapCeremonyClient_NilServices(t *testing.T) {
	for name, bundle := range map[string]*CeremonyServices{
		"nil bundle":      nil,
		"empty bundle":    {},
		"missing barrier": {Ceremony: (&CeremonyServices{}).Ceremony},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := newBootstrapCeremonyClient(nil, nil, bundle, nil, nil)
			require.Nil(t, client)
			var wireErr *CeremonyWiringError
			require.Truef(t, errors.As(err, &wireErr),
				"expected CeremonyWiringError, got %T: %v", err, err)
			require.Equal(t, "bootstrap-client services", wireErr.Stage)
		})
	}
}

// newBootstrapCeremonyClient must surface the qrdb embedded transport's
// typed error when the KV/Admin servicers are absent.
func TestNewBootstrapCeremonyClient_NilKVAdmin(t *testing.T) {
	bundle, err := buildBootstrapCeremonyServices(slog.Default(), t.TempDir(), testTrustDomain)
	require.NoError(t, err)

	client, err := newBootstrapCeremonyClient(nil, nil, bundle, nil, nil)
	require.Nil(t, client)
	var wireErr *CeremonyWiringError
	require.Truef(t, errors.As(err, &wireErr),
		"expected CeremonyWiringError, got %T: %v", err, err)
	require.Equal(t, "bootstrap-transport", wireErr.Stage)
}

// The bootstrap ceremony client must answer barrier verbs from the
// in-memory bootstrap barrier — the exact regression that broke every
// barrier CLI test when the node runs seal-disabled.
func TestNewBootstrapCeremonyClient_BarrierStatus(t *testing.T) {
	s := buildBootstrapClientFixture(t)

	status, err := s.bootstrapCeremonyClient.Barrier().BarrierStatus(context.Background())
	require.NoError(t, err, "barrier status over the bootstrap client must succeed")
	require.NotNil(t, status)
}

// buildBarrierCeremonyServices must route the barrier facade through the
// bootstrap ceremony client when the node runs without a seal barrier.
func TestBuildBarrierCeremonyServices_BootstrapClient(t *testing.T) {
	s := buildBootstrapClientFixture(t)

	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	require.NotNil(t, barrierSvc)
	require.NotNil(t, ceremonySvc)

	info, err := barrierSvc.BarrierStatus(context.Background())
	require.NoError(t, err, "facade barrier status must reach the bootstrap barrier")
	require.NotNil(t, info)
}

// The seal-disabled wiring must serve barrier verbs from the PERSISTENT
// data-plane barrier whose status names the configured strategy even
// before initialization — the contract TestBarrierStatusJSON asserts
// over every transport.
func TestBootstrapWiring_PersistentBarrierStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal = config.DefaultSealConfig() // Enabled=false, Strategy=passphrase

	dpSvc, rawBarrier, _, dpCloser, err := buildDataPlaneBarrierService(cfg, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, dpSvc)
	require.NotNil(t, rawBarrier)
	t.Cleanup(func() { _ = dpCloser.Close() })

	bundle, err := buildBootstrapCeremonyServices(slog.Default(), cfg.Storage.DataDir, testTrustDomain)
	require.NoError(t, err)
	bundle.Barrier = dpSvc

	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	s := &Server{logger: buildTestLogger(t), qrdbNode: node}

	client, err := newBootstrapCeremonyClient(
		s.GetQRDBKVServicer(), s.GetQRDBAdminServicer(), bundle, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	status, err := client.Barrier().BarrierStatus(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, status.Strategy,
		"uninitialized persistent barrier must report the configured strategy")
	require.Equal(t, "passphrase", status.Strategy)
}

// qrdbBarrierServicer must serve the qrdb.v1 barrier surface from the
// bootstrap ceremony client in the seal-disabled state instead of the
// node client (which would return NilServiceError for every call).
func TestQRDBBarrierServicer_BootstrapClient(t *testing.T) {
	s := buildBootstrapClientFixture(t)

	servicer := s.qrdbBarrierServicer()
	require.NotNil(t, servicer)

	info, err := servicer.BarrierStatus(context.Background())
	require.NoError(t, err, "qrdb.v1 barrier status must reach the bootstrap barrier")
	require.NotNil(t, info)
}
