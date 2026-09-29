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

// Package main: targeted coverage for pure helpers in agent_serve.go.
// The full runFleetServe lifecycle needs a live xkms/orchestrator stack
// and is exercised in integration tests.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// splitAndTrim
// =============================================================================

func TestSplitAndTrim_Empty(t *testing.T) {
	assert.Nil(t, splitAndTrim(""))
}

func TestSplitAndTrim_SingleNoSpaces(t *testing.T) {
	assert.Equal(t, []string{"alpha"}, splitAndTrim("alpha"))
}

func TestSplitAndTrim_MultipleWithSpaces(t *testing.T) {
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, splitAndTrim("  alpha,beta  ,gamma "))
}

func TestSplitAndTrim_DropsEmpty(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitAndTrim("a,,b,"))
}

// =============================================================================
// fleetServeLogger — the minimal Logger implementation. Smoke-test each
// method so the package interface stays satisfied and the lines are
// covered.
// =============================================================================

func TestAgentServeLogger_InterfaceCompliance(t *testing.T) {
	var _ interfaces.Logger = (*fleetServeLogger)(nil)
}

func TestAgentServeLogger_AllMethods(t *testing.T) {
	l := newFleetServeLogger()
	require.NotNil(t, l)

	// Each method must accept its expected argument shape without panic.
	l.Debug("d", "x", 1)
	l.Debugf("debug %d", 1)
	l.Info("i", "y")
	l.Infof("info %d", 2)
	l.Warn("w")
	l.Warnf("warn %d", 3)
	l.Error("e")
	l.Errorf("error %d", 4)
	// Fatal/Fatalf call os.Exit — do not invoke them.

	// Chainable variants return a logger.
	assert.NotNil(t, l.WithField("k", "v"))
	assert.NotNil(t, l.WithFields(map[string]interface{}{"k": "v"}))
	assert.NotNil(t, l.WithError(errAssertCanned))
}

// errAssertCanned is a cheap typed error used by the chainable test above.
type cannedAssertError string

func (e cannedAssertError) Error() string { return string(e) }

var errAssertCanned = cannedAssertError("test")
