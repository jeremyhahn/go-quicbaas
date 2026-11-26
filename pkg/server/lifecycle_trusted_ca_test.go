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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/pkg/api/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/caadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// The CA adapter the embedded, Unix and MCP transports serve does not depend
// on the rest build tag: this file carries no tag, so it runs in a build
// without REST too.
func TestTrustedCAService_ServesTheAdoptedCAWithoutTheRESTTag(t *testing.T) {
	assert.Nil(t, (&Server{}).trustedCAService(), "no CA adopted, no adapter")

	srv := &Server{xkmsCA: newFakeTrustedCA(t), config: &config.Config{}}
	ca := srv.trustedCAService()
	require.NotNil(t, ca)
	_, ok := ca.(*caadapter.XKMSCAServicer)
	assert.True(t, ok, "the adapter is the shared go-xkms CA servicer, not a REST-only type")
	// The embedded transport's Security().TCGCA() type-asserts the same adapter.
	_, ok = ca.(xkmstransport.TCGCAService)
	assert.True(t, ok, "the wired adapter also serves go-xkms's TCGCAService")
}
