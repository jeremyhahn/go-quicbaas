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
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// mockSystemIdentity names the system identity for an embedded client over a
// mock service. The mock has no embedded qrdb node, and a server without one
// binds no principal, so calls reach the mock as the system tenant.
func mockSystemIdentity() quicbaas.Option {
	return quicbaas.WithEmbeddedIdentity(quicbaas.ServicePrincipalIdentity(&server.Server{}))
}
