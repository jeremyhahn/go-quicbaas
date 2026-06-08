//go:build quic && !iam

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
	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
)

// configureQUICAuthz is a no-op when the iam build tag is absent. Without IAM
// there is no OIDC provider to validate tokens and no qrdb authorizer adapter,
// so the QUIC handler keeps its legacy allow-all behaviour.
func (s *Server) configureQUICAuthz(_ *quicapi.Server) {
	s.logger.Debug("QUIC authz not compiled in (iam build tag absent)")
}
