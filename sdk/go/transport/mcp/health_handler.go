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

// health_handler.go adapts the health client's Liveness and Readiness verbs
// to the quicbaas_liveness and quicbaas_readiness MCP tools and decodes their
// results into the *health.HealthResponse the SDK client type-asserts on.
// Ping has no tool of its own and keeps the generic path.

package mcp

import (
	healthclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// healthServiceName is the service name the health client sends.
const healthServiceName = "quicbaas.Health"

// isMCPHealthService reports whether the service name targets health.
func isMCPHealthService(service string) bool {
	return service == healthServiceName || service == "Health"
}

// healthVerbs are the health methods whose result needs the client type.
// Keep in sync with pkg/api/mcp/tools.go (executeLiveness, executeReadiness).
var healthVerbs = map[string]toolVerb{
	"Liveness": {
		tool:   "quicbaas_liveness",
		args:   healthNoArgs,
		check:  requireHealthBool("Alive"),
		decode: decodeHealthLiveness,
	},
	"Readiness": {
		tool:   "quicbaas_readiness",
		args:   healthNoArgs,
		check:  requireHealthBool("Ready"),
		decode: decodeHealthReadiness,
	},
}

// healthNoArgs serves the health tools, which take no arguments.
func healthNoArgs(interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// requireHealthBool refuses a result whose probe field is missing or not a
// bool, so a malformed result never reads as an unhealthy server.
func requireHealthBool(name string) func(op string, m map[string]interface{}) error {
	return func(op string, m map[string]interface{}) error {
		if _, ok := m[name].(bool); !ok {
			return errors.New(errors.CodeInternal, op, "tool result has no boolean "+name)
		}
		return nil
	}
}

func decodeHealthLiveness(m map[string]interface{}) interface{} {
	alive := getResultBool(m, "Alive")
	status := "dead"
	if alive {
		status = "alive"
	}
	return &healthclient.HealthResponse{
		Healthy: alive,
		Status:  status,
	}
}

func decodeHealthReadiness(m map[string]interface{}) interface{} {
	ready := getResultBool(m, "Ready")
	status := "not ready"
	if ready {
		status = "ready"
	}
	details := make(map[string]interface{})
	if reason := getResultString(m, "Reason"); reason != "" {
		details["reason"] = reason
	}
	return &healthclient.HealthResponse{
		Healthy: ready,
		Status:  status,
		Details: details,
	}
}
