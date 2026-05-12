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

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// newAPIInferenceTestService constructs an agentServiceImpl backed by the
// shared in-memory store and a silent logger. It is the unit-scope wiring
// used by the RegisterAPIInferenceBackend tests.
func newAPIInferenceTestService(t *testing.T) *agentServiceImpl {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService failed: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("AgentService is not *agentServiceImpl: %T", svc)
	}
	return impl
}

func TestRegisterAPIInferenceBackend_FromWrapped(t *testing.T) {
	t.Parallel()

	svc := newAPIInferenceTestService(t)

	wrapped := &stubInferenceBackend{
		provider: "anthropic",
		baseURL:  "https://api.anthropic.com",
		models: []inference.Model{
			{ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet"},
		},
	}

	err := svc.RegisterAPIInferenceBackend(context.Background(), &RegisterAPIInferenceBackendRequest{
		Name:         "anthropic-prod",
		Wrapped:      wrapped,
		DefaultModel: "claude-3-5-sonnet",
	})
	if err != nil {
		t.Fatalf("RegisterAPIInferenceBackend failed: %v", err)
	}

	infos, err := svc.ListBackends(context.Background())
	if err != nil {
		t.Fatalf("ListBackends failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(infos))
	}
	if infos[0].Name != "anthropic-prod" {
		t.Errorf("Name = %q, want %q", infos[0].Name, "anthropic-prod")
	}
	if infos[0].Type != "anthropic" {
		t.Errorf("Type = %q, want %q", infos[0].Type, "anthropic")
	}
	if infos[0].Capabilities.Kind != BackendKindAPIInference {
		t.Errorf("Kind = %q, want %q", infos[0].Capabilities.Kind, BackendKindAPIInference)
	}

	// ListBackendModels must work uniformly across all backend categories.
	models, err := svc.ListBackendModels(context.Background(), "anthropic-prod")
	if err != nil {
		t.Fatalf("ListBackendModels failed: %v", err)
	}
	if len(models) != 1 || models[0] != "claude-3-5-sonnet" {
		t.Errorf("models = %v, want [claude-3-5-sonnet]", models)
	}
}

func TestRegisterAPIInferenceBackend_FromAdapter(t *testing.T) {
	t.Parallel()

	svc := newAPIInferenceTestService(t)
	adapter, err := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "openai"},
	})
	if err != nil {
		t.Fatalf("NewAPIInferenceBackend failed: %v", err)
	}

	err = svc.RegisterAPIInferenceBackend(context.Background(), &RegisterAPIInferenceBackendRequest{
		Name:    "openai-prod",
		Backend: adapter,
	})
	if err != nil {
		t.Fatalf("RegisterAPIInferenceBackend failed: %v", err)
	}

	if got := svc.BackendToolGuidance("openai-prod"); got != "" {
		t.Errorf("ToolGuidance = %q, want empty", got)
	}
}

func TestRegisterAPIInferenceBackend_RejectsBothFields(t *testing.T) {
	t.Parallel()

	svc := newAPIInferenceTestService(t)
	adapter, _ := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{provider: "x"},
	})

	err := svc.RegisterAPIInferenceBackend(context.Background(), &RegisterAPIInferenceBackendRequest{
		Name:    "both",
		Backend: adapter,
		Wrapped: &stubInferenceBackend{provider: "y"},
	})
	if err == nil {
		t.Fatal("expected error when both Backend and Wrapped supplied")
	}
	if !errors.Is(err, ErrBackendNilRequired) {
		t.Errorf("expected ErrBackendNilRequired, got %v", err)
	}
}

func TestRegisterAPIInferenceBackend_RejectsNeither(t *testing.T) {
	t.Parallel()

	svc := newAPIInferenceTestService(t)
	err := svc.RegisterAPIInferenceBackend(context.Background(), &RegisterAPIInferenceBackendRequest{
		Name: "empty",
	})
	if err == nil {
		t.Fatal("expected error when neither Backend nor Wrapped supplied")
	}
	if !errors.Is(err, ErrBackendNilRequired) {
		t.Errorf("expected ErrBackendNilRequired, got %v", err)
	}
}

func TestRegisterAPIInferenceBackend_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	svc := newAPIInferenceTestService(t)
	err := svc.RegisterAPIInferenceBackend(context.Background(), &RegisterAPIInferenceBackendRequest{
		Wrapped: &stubInferenceBackend{provider: "x"},
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrBackendNameRequired) {
		t.Errorf("expected ErrBackendNameRequired, got %v", err)
	}
}

func TestRegisterAPIInferenceBackend_RejectsNilRequest(t *testing.T) {
	t.Parallel()

	svc := newAPIInferenceTestService(t)
	err := svc.RegisterAPIInferenceBackend(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}
