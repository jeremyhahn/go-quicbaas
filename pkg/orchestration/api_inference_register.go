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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// RegisterAPIInferenceBackendRequest contains parameters for registering an
// API-based inference backend (Anthropic, OpenAI, xAI, ...).
//
// Exactly one of Backend (an already-adapted AgentBackend) or Wrapped (a raw
// inference.APIInferenceBackend, which the service wraps internally) must be
// supplied. When Wrapped is supplied, the service constructs an
// APIInferenceBackend adapter using DefaultModel.
type RegisterAPIInferenceBackendRequest struct {
	// Name is the logical backend name advertised to clients (e.g.
	// "anthropic-prod"). Required.
	Name string `json:"name"`

	// Backend, if non-nil, is registered directly. Use this when the caller
	// has already constructed an APIInferenceBackend adapter and wants full
	// control over its configuration.
	Backend *APIInferenceBackend `json:"-"`

	// Wrapped, if non-nil, is the raw provider backend; the service wraps
	// it in an APIInferenceBackend adapter using DefaultModel and registers
	// the result. Convenient one-shot registration path for daemons reading
	// from config.
	Wrapped inference.APIInferenceBackend `json:"-"`

	// DefaultModel is forwarded to the adapter when Wrapped is supplied.
	// Ignored when Backend is supplied (the caller already configured it).
	DefaultModel string `json:"default_model,omitempty"`
}

// RegisterAPIInferenceBackend adds an API-based frontier-model inference
// backend to the service. The backend is adapted into the AgentBackend
// interface and registered via the same backends map used by ACP and
// model-server backends, so all downstream lookup, ListBackends, and
// ListBackendModels paths work uniformly.
//
// Validation:
//   - Name must be non-empty.
//   - Exactly one of Backend or Wrapped must be supplied.
func (s *agentServiceImpl) RegisterAPIInferenceBackend(ctx context.Context, req *RegisterAPIInferenceBackendRequest) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if req == nil {
		return &AgentServiceError{Sentinel: ErrBackendNilRequired}
	}
	if req.Name == "" {
		return &AgentServiceError{Sentinel: ErrBackendNameRequired}
	}

	if (req.Backend == nil) == (req.Wrapped == nil) {
		// Both nil OR both set — neither is acceptable.
		return &AgentServiceError{
			Sentinel: ErrBackendNilRequired,
			Message:  "exactly one of Backend or Wrapped must be supplied",
		}
	}

	adapter := req.Backend
	if adapter == nil {
		var err error
		adapter, err = NewAPIInferenceBackend(APIInferenceBackendConfig{
			Wrapped:      req.Wrapped,
			DefaultModel: req.DefaultModel,
		})
		if err != nil {
			return err
		}
	}

	s.backends.Store(req.Name, AgentBackend(adapter))

	s.logger.Debugf(
		"registered api inference backend: name=%s provider=%s base_url=%s",
		req.Name,
		adapter.Wrapped().Provider(),
		adapter.Wrapped().BaseURL(),
	)

	return nil
}
