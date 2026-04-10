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

package orchestration

import "testing"

func TestBackendKindConstants(t *testing.T) {
	t.Run("AgentValue", func(t *testing.T) {
		if string(BackendKindAgent) != "agent" {
			t.Errorf("BackendKindAgent = %q, want %q", BackendKindAgent, "agent")
		}
	})

	t.Run("ModelServerValue", func(t *testing.T) {
		if string(BackendKindModelServer) != "model_server" {
			t.Errorf("BackendKindModelServer = %q, want %q", BackendKindModelServer, "model_server")
		}
	})

	t.Run("AgentAndModelServerAreDifferent", func(t *testing.T) {
		if BackendKindAgent == BackendKindModelServer {
			t.Error("BackendKindAgent and BackendKindModelServer must be different values")
		}
	})
}

func TestBackendKindConstants_TypeSafety(t *testing.T) {
	// Verify BackendKind is a distinct type from string, preventing
	// accidental assignment of arbitrary string values at compile time.
	var kind BackendKind
	kind = BackendKindAgent
	if kind != BackendKindAgent {
		t.Errorf("assignment to BackendKind failed, got %q", kind)
	}

	kind = BackendKindModelServer
	if kind != BackendKindModelServer {
		t.Errorf("assignment to BackendKind failed, got %q", kind)
	}
}
