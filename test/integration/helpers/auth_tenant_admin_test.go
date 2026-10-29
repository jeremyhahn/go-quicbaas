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

//go:build integration

package helpers

import (
	"context"
	"testing"
)

// TestRequestTimeout_FollowsCallerDeadline checks a request made under a
// setup budget gets the rest of that budget, not a fixed 30s that cut a slow
// provision short in Integration 71951.
func TestRequestTimeout_FollowsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), tenantSetupTimeout)
	defer cancel()

	got := requestTimeout(ctx)
	if got <= defaultRequestTimeout || got > tenantSetupTimeout {
		t.Errorf("requestTimeout = %v, want the remaining setup budget (<= %v, > %v)", got, tenantSetupTimeout, defaultRequestTimeout)
	}
}

// TestRequestTimeout_NoDeadlineUsesDefault checks a caller without a deadline
// still gets a bounded request.
func TestRequestTimeout_NoDeadlineUsesDefault(t *testing.T) {
	if got := requestTimeout(context.Background()); got != defaultRequestTimeout {
		t.Errorf("requestTimeout = %v, want %v", got, defaultRequestTimeout)
	}
}
