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
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCRepoOnboardingHandler_ReachesTheServersPath(t *testing.T) {
	if got := sdlcRepoOnboardingPath("web") + tenantQuery("t"); got != "/api/v1/sdlc/repos/web/onboarding?tenant_id=t" {
		t.Errorf("repo onboarding path = %q", got)
	}
	// A repo name is operator input and must not reshape the path.
	if got := sdlcRepoOnboardingPath("a/b"); got != "/api/v1/sdlc/repos/a%2Fb/onboarding" {
		t.Errorf("repo onboarding path with a slash = %q, want the name escaped", got)
	}
}

func TestSDLCRepoOnboardingHandler_RefusesTheWrongRequestType(t *testing.T) {
	h := &sdlcRESTHandler{}
	if _, err := h.handleOnboardRepo(context.Background(), &sdlc.OnboardRequest{}); err == nil {
		t.Error("OnboardRepo accepted the wrong request type")
	}
}
