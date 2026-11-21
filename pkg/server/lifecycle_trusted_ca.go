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
	"github.com/jeremyhahn/go-quicbaas/pkg/api/caadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// trustedCAService returns the go-xkms CA adapter every transport serves:
// the REST /api/v1/xkms/ca/* routes, the MCP xkms.ca.* tools and the embedded
// transport's Security().TrustedCA() (#1330). The adapter lives in the
// untagged pkg/api/caadapter, so a build without the rest tag still serves
// the CA. It returns nil before a CA is adopted.
func (s *Server) trustedCAService() service.TrustedCAService {
	if s.xkmsCA == nil {
		return nil
	}
	return caadapter.NewXKMSCAService(s.xkmsCA, s.config.IAM.SPIFFETrustDomain)
}
