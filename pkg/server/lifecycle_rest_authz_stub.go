//go:build rest && !iam

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
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// configureRESTAuthz is a no-op when the iam build tag is absent. Without IAM
// there is no OIDC provider to validate tokens against, so the REST server
// starts without the authorization middleware (the same behaviour as before
// authz wiring existed).
func (s *Server) configureRESTAuthz(_ *restapi.Server) {
	s.logger.Debug("REST authz not compiled in (iam build tag absent)")
}
