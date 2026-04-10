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

//go:build !iam

package service

import "github.com/jeremyhahn/go-quicbaas/pkg/interfaces"

// SetIAMProvider sets the IAM components provider for REST handlers.
// This stub does nothing when IAM is not enabled.
func (s *quicBaaS) SetIAMProvider(_ IAMComponents) {
	// No-op when IAM is not enabled
}

// GetInternalAuthManager returns the internal auth manager for MCP and other
// internal operations. Returns nil when IAM is not enabled.
func (s *quicBaaS) GetInternalAuthManager() interfaces.AuthManager {
	return nil
}
