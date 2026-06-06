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

//go:build rest && iam

// configure_rest_authz_branches_test.go covers the OIDC-provider-unavailable
// early-return branch of configureRESTAuthz: when the service implements
// oidcProviderGetter but the provider is nil (IAM not yet initialised), authz
// wiring is skipped rather than installed against a nil provider.

package server

import (
	"context"
	"testing"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// oidcGetterService embeds service.QuicBaaS (nil) so it satisfies the
// quicBaaSService interface, and implements oidcProviderGetter so
// configureRESTAuthz takes the getter path. GetOIDCProvider returns the
// configured provider/err; none of the embedded methods are reached.
type oidcGetterService struct {
	service.QuicBaaS
	provider *iam.OIDCProvider
	err      error
}

func (s *oidcGetterService) GetOIDCProvider(context.Context) (*iam.OIDCProvider, error) {
	return s.provider, s.err
}

// TestConfigureRESTAuthz_NilProviderSkips covers the provider-unavailable branch:
// a getter that returns (nil, nil) must make configureRESTAuthz return early
// without panicking on the subsequent validator/RBAC wiring.
func TestConfigureRESTAuthz_NilProviderSkips(t *testing.T) {
	s := &Server{
		logger:          buildTestLogger(t),
		quicBaaSService: &oidcGetterService{provider: nil, err: nil},
	}

	// restServer is unused on the early-return path; nil is safe here.
	var restServer *restapi.Server
	s.configureRESTAuthz(restServer)
}
