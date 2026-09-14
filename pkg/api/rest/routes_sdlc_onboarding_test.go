//go:build rest

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

package rest

import (
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// TestSDLCWebhookPrefix_MatchesTheMountedRoute is a DRIFT GATE over the one
// string that leaves this process and lands in somebody else's
// infrastructure.
//
// Onboarding writes <public_url> + sdlc.WebhookPathPrefix + <projectID> into
// a webhook on the forge. If that prefix ever stops matching the route the
// router actually mounts, every managed repository is provisioned to deliver
// into a 404 — and the failure is SILENT on both sides: the forge records a
// delivery it made, and the node never sees one. Twenty-eight repositories
// would have to be re-onboarded to fix it.
//
// The gate is cheap because the mistake is not: the two live in different
// packages and nothing else would ever compare them.
func TestSDLCWebhookPrefix_MatchesTheMountedRoute(t *testing.T) {
	const want = "POST /api/v1/sdlc/webhook/{projectID}"

	var mounted string
	for _, route := range Routes() {
		if route.Method == "POST" && strings.HasPrefix(route.Pattern, sdlc.WebhookPathPrefix) {
			mounted = route.Method + " " + route.Pattern
			break
		}
	}
	if mounted == "" {
		t.Fatalf("no mounted route begins with sdlc.WebhookPathPrefix (%q); every provisioned webhook would deliver into a 404",
			sdlc.WebhookPathPrefix)
	}
	if mounted != want {
		t.Errorf("webhook ingress route = %q, want %q", mounted, want)
	}
	if !strings.HasSuffix(sdlc.WebhookPathPrefix, "/") {
		t.Errorf("WebhookPathPrefix = %q, want a trailing separator so the project id is its own path segment",
			sdlc.WebhookPathPrefix)
	}
}

// TestSDLCOnboardingRoutes_SplitReadFromWrite pins the permission split.
//
// The read must never carry a write permission and the write must never be
// reachable with only read: an operator auditing twenty-eight repositories
// needs to be able to ASK without being able to change, and the whole value
// of the conformance verb rests on that being true.
func TestSDLCOnboardingRoutes_SplitReadFromWrite(t *testing.T) {
	cases := map[string]struct{ resource, action string }{
		"POST /api/v1/sdlc/projects/{projectID}/onboarding": {"sdlc", "bind"},
		"GET /api/v1/sdlc/projects/{projectID}/conformance": {"sdlc", "read"},
	}
	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			rule, ok := frozenRoutePermissions[key]
			if !ok {
				t.Fatalf("route %q carries no permission rule; the authz gate is default-deny and it would be unreachable", key)
			}
			if rule.auth != RouteAuthPermission {
				t.Fatalf("route %q auth = %v, want a permission rule", key, rule.auth)
			}
			if rule.resource != want.resource || rule.action != want.action {
				t.Errorf("route %q = %s:%s, want %s:%s", key, rule.resource, rule.action, want.resource, want.action)
			}
		})
	}
}
