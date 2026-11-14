//go:build rag

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

package rag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// NewSystem error paths that can be triggered through configuration
// =============================================================================

// TestNewSystemPipelineValidationError tests the pipeline creation error path
// by passing a DefaultTopK of 0 which passes config validation but fails
// pipeline.Config.Validate().
func TestNewSystemPipelineValidationError(t *testing.T) {
	cfg := createTestRAGConfig(t)
	// Set DefaultTopK to 0 - passes config.PipelineConfig.Validate() which checks < 0
	// but fails pipeline.Config.Validate() which checks <= 0
	cfg.Pipeline.DefaultTopK = 0

	system, err := NewSystem(cfg)

	// Should fail during pipeline creation
	assert.Error(t, err)
	assert.Nil(t, system)
	assert.Contains(t, err.Error(), "failed to create pipeline")
}

// TestNewSystemWithVeryShortTimeout tests system creation with short timeout

// =============================================================================
// Test various valid configurations to improve coverage
// =============================================================================

// =============================================================================
// Test System with specific configuration paths
// =============================================================================

func TestNewSystemWithDifferentTemplates(t *testing.T) {
	// Use valid templates: markdown, json, xml, plain
	templates := []string{"markdown", "json", "xml", "plain"}

	for _, template := range templates {
		t.Run("template_"+template, func(t *testing.T) {
			cfg := createTestRAGConfig(t)
			cfg.Context.DefaultTemplate = template

			system, err := NewSystem(cfg)

			require.NoError(t, err)
			assert.NotNil(t, system)
			system.Close()
		})
	}
}
