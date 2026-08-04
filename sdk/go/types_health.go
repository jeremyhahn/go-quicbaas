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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Health-probe re-exports. QuicBaaS is the single SDK frontage for the
// stack: downstream consumers (e.g. go-trusted-platform) reach the shared
// qrdb health-probe framework through this SDK rather than importing
// go-qrdb directly. Pure aliases — no transport state, no behavior.

// HealthCheckStatus is the health state reported by a check.
type HealthCheckStatus = qrdbsdk.HealthCheckStatus

const (
	// HealthCheckStatusHealthy indicates the component is fully operational.
	HealthCheckStatusHealthy = qrdbsdk.HealthCheckStatusHealthy
	// HealthCheckStatusUnhealthy indicates the component is not operational.
	HealthCheckStatusUnhealthy = qrdbsdk.HealthCheckStatusUnhealthy
	// HealthCheckStatusDegraded indicates partial functionality.
	HealthCheckStatusDegraded = qrdbsdk.HealthCheckStatusDegraded
)

// HealthCheckResult is the outcome of a single health check.
type HealthCheckResult = qrdbsdk.HealthCheckResult

// HealthCheckFunc is a registered health-check callback.
type HealthCheckFunc = qrdbsdk.HealthCheckFunc

// HealthChecker aggregates named checks into Kubernetes-style liveness,
// readiness, and startup probes.
type HealthChecker = qrdbsdk.HealthChecker

// NewHealthChecker constructs an empty HealthChecker.
var NewHealthChecker = qrdbsdk.NewHealthChecker

// AggregateHealthCheckStatus reduces a set of check results to one status.
var AggregateHealthCheckStatus = qrdbsdk.AggregateHealthCheckStatus
