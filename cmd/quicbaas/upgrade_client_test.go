// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package main: coverage for createUpgradeGRPCClient TLS-flag validation and
// reconcileUpgradeSDKConfig, which feeds the upgrade command's transport flags
// into the global SDK configuration so the SDK client is built with TLS.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withUpgradeTLSGlobals installs clean upgrade TLS globals and restores
// the originals on cleanup.
func withUpgradeTLSGlobals(t *testing.T) {
	t.Helper()
	oc, ok, oa := upgradeTLSCert, upgradeTLSKey, upgradeTLSCA
	t.Cleanup(func() {
		upgradeTLSCert = oc
		upgradeTLSKey = ok
		upgradeTLSCA = oa
	})
	upgradeTLSCert = ""
	upgradeTLSKey = ""
	upgradeTLSCA = ""
}

// withUpgradeSDKConfigGlobals snapshots and clears every global the
// reconcileUpgradeSDKConfig path reads or writes, restoring them on cleanup so
// the SDK-config tests do not leak state across the suite.
func withUpgradeSDKConfigGlobals(t *testing.T) {
	t.Helper()
	osa, osca, oscert, oskey := upgradeServerAddress, upgradeTLSCA, upgradeTLSCert, upgradeTLSKey
	oa, oca, ocert, okey := address, tlsCA, tlsCert, tlsKey
	t.Cleanup(func() {
		upgradeServerAddress = osa
		upgradeTLSCA = osca
		upgradeTLSCert = oscert
		upgradeTLSKey = oskey
		address = oa
		tlsCA = oca
		tlsCert = ocert
		tlsKey = okey
	})
	upgradeServerAddress = ""
	upgradeTLSCA = ""
	upgradeTLSCert = ""
	upgradeTLSKey = ""
	address = ""
	tlsCA = ""
	tlsCert = ""
	tlsKey = ""
}

func TestCreateUpgradeGRPCClient_RequiresTLS(t *testing.T) {
	withUpgradeTLSGlobals(t)
	_, err := createUpgradeGRPCClient("localhost:50051")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS is required")
}

func TestCreateUpgradeGRPCClient_CAOnlyBadCA(t *testing.T) {
	withUpgradeTLSGlobals(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(bad, []byte("not a PEM"), 0o600))
	upgradeTLSCA = bad
	_, err := createUpgradeGRPCClient("localhost:50051")
	require.Error(t, err)
}

// TestReconcileUpgradeSDKConfig_PromotesUpgradeFlags proves the regression fix:
// when the upgrade subcommand parsed --tls-ca / --tls-cert / --tls-key / --server
// into its OWN (shadowing) flag vars, reconcileUpgradeSDKConfig promotes them
// onto the global SDK config vars so createSDKClient -> buildTLSConfig builds a
// non-nil TLS config. Without this, the SDK gRPC transport rejects the dial
// with "TLS configuration is required for all connections".
func TestReconcileUpgradeSDKConfig_PromotesUpgradeFlags(t *testing.T) {
	withUpgradeSDKConfigGlobals(t)

	caPath := writeThrowawayCA(t)
	upgradeTLSCA = caPath
	upgradeServerAddress = "localhost:50051"

	// Sanity: with the global TLS vars empty, no TLS config is produced.
	cfg, err := buildTLSConfig()
	require.NoError(t, err)
	require.Nil(t, cfg, "no global --tls-ca means no SDK TLS config (the bug)")

	reconcileUpgradeSDKConfig()

	assert.Equal(t, caPath, tlsCA, "upgrade --tls-ca must feed the global SDK config")
	assert.Equal(t, "localhost:50051", address, "upgrade --server must feed the global address")

	cfg, err = buildTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg, "SDK client built via the upgrade path must carry a TLS config")
	require.NotNil(t, cfg.RootCAs, "the upgrade --tls-ca must populate the SDK trust pool")
}

// TestReconcileUpgradeSDKConfig_GlobalWins ensures an explicitly set global
// flag is never clobbered by the upgrade-specific flag (the upgrade flag is a
// fallback, not an override).
func TestReconcileUpgradeSDKConfig_GlobalWins(t *testing.T) {
	withUpgradeSDKConfigGlobals(t)

	tlsCA = "/global/ca.pem"
	upgradeTLSCA = "/upgrade/ca.pem"

	reconcileUpgradeSDKConfig()

	assert.Equal(t, "/global/ca.pem", tlsCA, "an explicit global --tls-ca must win over the upgrade flag")
}

// TestReconcileUpgradeSDKConfig_NoFlagsNoChange ensures reconciliation is inert
// when the upgrade-specific flags are unset, leaving the global config (already
// populated by the persistent --tls-ca) untouched.
func TestReconcileUpgradeSDKConfig_NoFlagsNoChange(t *testing.T) {
	withUpgradeSDKConfigGlobals(t)

	tlsCA = "/global/ca.pem"
	address = "global-host:50051"

	reconcileUpgradeSDKConfig()

	assert.Equal(t, "/global/ca.pem", tlsCA)
	assert.Equal(t, "global-host:50051", address)
}
