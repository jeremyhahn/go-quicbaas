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

package agent

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus metrics for the agent runtime. Names follow the
// quicbaas_agent_runtime_<what>_<unit> convention.
//
// Collectors are constructed at package-load time so every read from the
// hot path (Spawn, handler instrumentation) sees an immutable pointer and
// needs no synchronization. RegisterMetrics wires them into the caller's
// chosen registry.
var (
	// SpawnsTotal counts every successful Spawn dispatch by the daemon.
	SpawnsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "spawns_total",
			Help:      "Total agent session spawns dispatched by the runtime.",
		},
		[]string{"tenant", "backend", "sandbox"},
	)

	// ActiveSessions tracks the current live session count per tenant/backend.
	ActiveSessions = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "active_sessions",
			Help:      "Currently live agent sessions by tenant and backend.",
		},
		[]string{"tenant", "backend"},
	)

	// RPCDuration observes end-to-end handler latency for every agent-runtime
	// RPC. The "code" label holds the gRPC code string (e.g., "OK", "Internal").
	RPCDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "rpc_duration_seconds",
			Help:      "Latency of agent-runtime RPC handlers in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"rpc", "code"},
	)

	// ImagePullsTotal counts container-image pulls performed during daemon
	// startup. The "result" label is "ok" or "error".
	ImagePullsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "image_pulls_total",
			Help:      "Container image pulls performed by the daemon at startup.",
		},
		[]string{"result"},
	)
)

const (
	metricsNamespace = "quicbaas"
	metricsSubsystem = "agent_runtime"
)

// ErrMetricsAlreadyRegistered is returned by RegisterMetrics when the
// collectors are already present in the target registry. Callers that
// want strict uniqueness should check for this error; callers that want
// idempotent registration can ignore it.
var ErrMetricsAlreadyRegistered = errors.New("agent: metrics already registered")

// ErrMetricsNilRegistry is returned when RegisterMetrics receives nil.
var ErrMetricsNilRegistry = errors.New("agent: metrics registry is nil")

// RegisterMetrics registers the agent runtime collectors with the given
// Registerer. Production callers pass prometheus.DefaultRegisterer; tests
// construct a fresh prometheus.NewRegistry to avoid global state.
//
// Returns ErrMetricsAlreadyRegistered if the supplied registry already
// holds an equivalent collector. Any other registration failure (e.g.,
// a different collector shares the same name) is returned unchanged.
func RegisterMetrics(reg prometheus.Registerer) error {
	if reg == nil {
		return ErrMetricsNilRegistry
	}
	for _, c := range []prometheus.Collector{SpawnsTotal, ActiveSessions, RPCDuration, ImagePullsTotal} {
		if err := reg.Register(c); err != nil {
			var already prometheus.AlreadyRegisteredError
			if errors.As(err, &already) {
				return ErrMetricsAlreadyRegistered
			}
			return &MetricsRegisterError{Cause: err}
		}
	}
	return nil
}

// MetricsRegisterError wraps a non-already-registered failure from
// prometheus.Registerer.Register.
type MetricsRegisterError struct {
	Cause error
}

// Error implements the error interface.
func (e *MetricsRegisterError) Error() string {
	return "agent: metrics register: " + e.Cause.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *MetricsRegisterError) Unwrap() error { return e.Cause }

// sandboxLabel maps the runtime sandbox selector to a compact metric label.
func sandboxLabel(sandboxed bool) string {
	if sandboxed {
		return "sandboxed"
	}
	return "host"
}
