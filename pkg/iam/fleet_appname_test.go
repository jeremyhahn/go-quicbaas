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

package iam

import "testing"

// TestSanitizeAppName_TrimsAndReplacesReservedRunes asserts the
// canonical sanitiser the REST register handler and the deploy
// orchestrator both rely on. The behaviour is part of the agent
// register contract and changing it requires coordinated rollouts.
func TestSanitizeAppName_TrimsAndReplacesReservedRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "unknown"},
		{"whitespace only", "   \t\n", "unknown"},
		{"simple", "host.example.com", "host.example.com"},
		{"leading and trailing whitespace", "  agent-1 ", "agent-1"},
		{"spaces inside", "host with space", "host-with-space"},
		{"slash", "tenant/agent", "tenant-agent"},
		{"colon", "host:port", "host-port"},
		{"backslash", `bad\path`, "bad-path"},
		{"all reserved", " a / : \\ b ", "a-------b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeAppName(tc.in); got != tc.want {
				t.Fatalf("SanitizeAppName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestAgentAppName_Composes asserts the canonical AgentID shape used
// by both the server-side register handler and the client-side deploy
// orchestrator's wait-online matcher. Drift between the two would
// re-introduce the very production bug this code path eliminates.
func TestAgentAppName_Composes(t *testing.T) {
	cases := []struct {
		name   string
		tenant string
		cn     string
		want   string
	}{
		{"happy path", "default", "agent-198.51.100.91", "agent-default-agent-198.51.100.91"},
		{"tenant with dashes", "acme-prod", "agent-host", "agent-acme-prod-agent-host"},
		{"sanitises CN", "t", "host with space", "agent-t-host-with-space"},
		{"empty CN yields unknown", "t", "", "agent-t-unknown"},
		{"colon in CN", "t", "host:9091", "agent-t-host-9091"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FleetNodeAppName(tc.tenant, tc.cn); got != tc.want {
				t.Fatalf("FleetNodeAppName(%q,%q) = %q, want %q", tc.tenant, tc.cn, got, tc.want)
			}
		})
	}
}
