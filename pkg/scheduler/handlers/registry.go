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

// Package handlers provides built-in job handlers for the scheduler.
package handlers

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// Handler is the interface for all scheduler handlers.
type Handler interface {
	// Name returns the unique handler name for registration.
	Name() string

	// Handler returns the JobHandler function for execution.
	Handler() scheduler.JobHandler
}

// Registry manages handler registration and lookup.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry creates a new handler registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler to the registry.
func (r *Registry) Register(h Handler) {
	r.handlers[h.Name()] = h
}

// Get retrieves a handler by name.
func (r *Registry) Get(name string) (Handler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

// All returns a copy of all registered handlers.
func (r *Registry) All() map[string]Handler {
	result := make(map[string]Handler, len(r.handlers))
	for k, v := range r.handlers {
		result[k] = v
	}
	return result
}

// RegisterAll registers all handlers with a scheduler.
func (r *Registry) RegisterAll(s scheduler.Scheduler) error {
	for _, h := range r.handlers {
		if err := s.RegisterHandler(h.Name(), h.Handler()); err != nil {
			return err
		}
	}
	return nil
}
