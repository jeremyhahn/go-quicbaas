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

package providers

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// createABACProvider returns an error when ABAC is not available.
// This stub is compiled when the iam build tag is not set.
func createABACProvider(_ *FactoryConfig) (interfaces.AuthorizationProvider, error) {
	return nil, ErrABACNotAvailable
}

// IsABACAvailable returns false when ABAC is not available in this build.
// This function returns false when the iam build tag is not set.
func IsABACAvailable() bool {
	return false
}
