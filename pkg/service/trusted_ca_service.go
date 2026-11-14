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

package service

import (
	"sync"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TrustedCAService is the go-xkms CAService the server composes: the CA that
// Security().TrustedCA() re-exports on every transport (#1330). The REST
// /api/v1/xkms/ca/* routes, the MCP xkms.ca.* tools and the embedded
// transport all serve the same adapter.
type TrustedCAService = xkmstransport.CAService

// trustedCAHolder carries the CA the server wires after it loads it, which is
// later than the service layer is built.
type trustedCAHolder struct {
	mu sync.RWMutex
	ca TrustedCAService
}

// TrustedCA returns the go-xkms CA the server wired, or nil before the server
// loads one.
func (s *quicBaaS) TrustedCA() TrustedCAService {
	s.trustedCA.mu.RLock()
	defer s.trustedCA.mu.RUnlock()
	return s.trustedCA.ca
}

// SetTrustedCA wires the go-xkms CA the embedded transport serves.
func (s *quicBaaS) SetTrustedCA(ca TrustedCAService) {
	s.trustedCA.mu.Lock()
	defer s.trustedCA.mu.Unlock()
	s.trustedCA.ca = ca
}
