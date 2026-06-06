//go:build !iam || !rest

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

import restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"

// attachHomeRealmResolver is a no-op when the iam build tag is not set: there
// is no IAM home-realm directory to wire.
func (s *Server) attachHomeRealmResolver(_ *restapi.Config) {}
