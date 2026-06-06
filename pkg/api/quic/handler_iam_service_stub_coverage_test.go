//go:build quic && !iam

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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// When the iam build tag is off, the IAM/MFA service accessors are no-ops
// and the getters report nil. Exercise the setters/clearers so they don't
// silently change behaviour in a future refactor.
func TestIAMServiceStubs_Disabled(t *testing.T) {
	handler := createTestHandler()

	// Setters and clearers must not panic.
	handler.setIAMService("ignored")
	assert.Nil(t, handler.getIAMService())
	handler.clearIAMService()
	assert.Nil(t, handler.getIAMService())

	handler.setMFAService("ignored")
	assert.Nil(t, handler.getMFAService())
	handler.clearMFAService()
	assert.Nil(t, handler.getMFAService())
}
