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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestEmbedded_FleetList_ScopeDeniedIsTheTypedError pins that the service's
// fleet-listing refusals reach an in-process caller as the typed forbidden
// error, never as an empty listing (go-quicbaas#680).
func TestEmbedded_FleetList_ScopeDeniedIsTheTypedError(t *testing.T) {
	_, crossTenant := tenantctx.Target(tenantctx.WithSystem(context.Background()), "orchestration.ListFleetNodes", "acme")
	denials := map[string]error{
		"cross-tenant": crossTenant,
		"scan denied":  qerrors.New(qerrors.ErrForbidden, "fleet.ListFleetNodes", "scope denied"),
	}
	for name, denial := range denials {
		t.Run(name, func(t *testing.T) {
			svc := &orchMock{agent: &fakeAgentService{listAgentsErr: denial}}

			resp, err := handleFleetList(context.Background(), svc, &orchestratorSDK.ListFleetNodesRequest{TenantID: "acme"})
			require.Error(t, err)
			require.Nil(t, resp)
			require.Equal(t, qerrors.ErrForbidden, qerrors.Code(err))
		})
	}
}
