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

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// IAM Stub Tests (Non-IAM Build)
// =============================================================================

func TestQuicBaaS_SetIAMProvider_Stub(t *testing.T) {
	t.Run("does nothing when IAM is not enabled", func(t *testing.T) {
		db := &quicBaaS{}
		// Should not panic
		db.SetIAMProvider(nil)
	})
}

func TestQuicBaaS_IAM_Stub(t *testing.T) {
	t.Run("returns nil when IAM is not enabled", func(t *testing.T) {
		db := &quicBaaS{}
		result := db.IAM()
		assert.Nil(t, result)
	})
}

func TestQuicBaaS_GetInternalAuthManager_Stub(t *testing.T) {
	t.Run("returns nil when IAM is not enabled", func(t *testing.T) {
		db := &quicBaaS{}
		result := db.GetInternalAuthManager()
		assert.Nil(t, result)
	})
}
