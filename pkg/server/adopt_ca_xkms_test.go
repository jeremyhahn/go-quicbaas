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

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestAdoptCA_InstallsTheCAOnTheXKMSService proves the adopted CA reaches the
// global XKMS service: the TCP KeystoreService installs its CA servicer from
// that service, so without it the CA RPCs never reach the server's CA.
func TestAdoptCA_InstallsTheCAOnTheXKMSService(t *testing.T) {
	initOIDCSigningDefault(t)
	t.Cleanup(dbxkms.Reset)

	srv := &Server{config: &config.Config{}}
	srv.adoptCA(newRealSoftwareCA(t))

	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	require.NotNil(t, svc.CA(), "adoptCA must install the CA on the XKMS service")
}
