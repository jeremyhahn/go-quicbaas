//go:build !iam

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

import "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"

// installRemoteBackendResolver is the non-iam stub. Without IAM we have no
// way to authenticate the central's outbound daemon calls, so remote
// dispatch stays disabled in this build variant. Operators needing remote
// daemons must build with `-tags iam`.
func (s *Server) installRemoteBackendResolver(_ orchestration.AgentService) {
	s.logger.Debug("Remote backend resolver not installed: iam build tag required")
}
