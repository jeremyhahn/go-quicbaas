//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestCeremonyInitialize_AcceptsJSONEncodedArgs pins that the standalone
// qrdb_ceremony_initialize accepts officers and sans as JSON-encoded strings,
// the shape go-qrdb's native tool declares and the SDK sends.
func TestCeremonyInitialize_AcceptsJSONEncodedArgs(t *testing.T) {
	fake := &fakeCeremonyService{initInfo: &service.CeremonyInitInfo{State: "READY"}}
	r := newCeremonyTestRegistry(t, fake)

	_, err := r.ExecuteTool(context.Background(), "qrdb_ceremony_initialize", map[string]interface{}{
		"threshold": 1,
		"sans":      `["node1","alt"]`,
		"officers":  `[{"username":"alice","csr_pem":"csr-a"}]`,
	})
	require.NoError(t, err)
	require.NotNil(t, fake.gotInitReq)
	require.Equal(t, []string{"node1", "alt"}, fake.gotInitReq.SANs)
	require.Equal(t, []service.CeremonyOfficer{{Username: "alice", CSRPEM: "csr-a"}}, fake.gotInitReq.Officers)
}
