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

package circuitbreaker

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the Prometheus metrics for circuit breaker observability.
type Metrics struct {
	// StateTransitions counts circuit breaker state transitions.
	// Labels: name, from, to
	StateTransitions *prometheus.CounterVec

	// CallTotal counts the total number of calls by outcome.
	// Labels: name, result (success, failure, rejected)
	CallTotal *prometheus.CounterVec
}

// NewMetrics creates Prometheus metrics for circuit breaker monitoring and registers
// them with the provided registerer. If registerer is nil, the default Prometheus
// registerer is used.
func NewMetrics(registerer prometheus.Registerer) *Metrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	stateTransitions := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quicbaas_circuit_breaker_state_transitions_total",
			Help: "Total number of circuit breaker state transitions",
		},
		[]string{"name", "from", "to"},
	)

	callTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quicbaas_circuit_breaker_calls_total",
			Help: "Total number of circuit breaker calls by outcome",
		},
		[]string{"name", "result"},
	)

	registerer.MustRegister(stateTransitions)
	registerer.MustRegister(callTotal)

	return &Metrics{
		StateTransitions: stateTransitions,
		CallTotal:        callTotal,
	}
}

// Apply configures the given circuit breaker with these metrics.
func (m *Metrics) Apply(cb *CircuitBreaker) {
	if m == nil || cb == nil {
		return
	}
	cb.SetMetrics(m.StateTransitions, m.CallTotal)
}
