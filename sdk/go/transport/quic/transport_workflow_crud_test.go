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

package quic

import (
	"testing"

	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/workflowcrud"
)

// TestTransport_WorkflowCRUDOverHTTP3 drives the workflow CRUD verbs over
// HTTP/3 to the REST workflow routes: QUIC reaches them through the REST
// handler set.
func TestTransport_WorkflowCRUDOverHTTP3(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, workflowcrud.HTTPHandler(workflowcrud.NewStore().Agent()))
	defer shutdown()
	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	workflowcrud.Exercise(t, orchestratorclient.NewClient(tr), errors.IsConflict)
}
