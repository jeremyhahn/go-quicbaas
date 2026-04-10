//go:build iam

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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestResetIAMFactory_RestoresDefault(t *testing.T) {
	// Save original
	original := iamClientFactory
	defer func() {
		iamClientFactory = original
	}()

	// Override factory
	iamClientFactory = nil

	// Reset should restore
	resetIAMFactory()

	// Verify it's not nil anymore
	assert.NotNil(t, iamClientFactory)
}

// TestCreateIAMClient_FactoryFunction tests the factory convenience function.
func TestCreateIAMClient_FactoryFunction(t *testing.T) {
	// Save original
	original := iamClientFactory
	defer func() {
		iamClientFactory = original
	}()

	// Test that factory is called
	called := false
	iamClientFactory = func() (iam.Client, error) {
		called = true
		return nil, assert.AnError
	}

	// Call the convenience function - it will call our factory
	_, _ = createIAMClient()

	assert.True(t, called, "factory should have been called")
}
