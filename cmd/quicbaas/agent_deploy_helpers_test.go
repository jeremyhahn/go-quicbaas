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

// Package main: coverage for the simple helpers in agent_deploy.go.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeployNoCertIssuerError_Message(t *testing.T) {
	e := &deployNoCertIssuerError{}
	assert.Contains(t, e.Error(), "agent deploy")
	assert.Contains(t, e.Error(), "no certificate issuer available")
}

// TestDefaultResolveAgentCertIssuer_RejectsWithoutAddress exercises the
// happy path through defaultResolveAgentCertIssuer when --address is
// empty (the underlying newSDKAgentCertIssuer returns the typed error).
func TestDefaultResolveAgentCertIssuer_RejectsWithoutAddress(t *testing.T) {
	withAddress(t, "")
	_, err := defaultResolveAgentCertIssuer()
	if err == nil {
		t.Fatal("expected error from defaultResolveAgentCertIssuer when --address is unset")
	}
	assert.Contains(t, err.Error(), "--address is required")
}
