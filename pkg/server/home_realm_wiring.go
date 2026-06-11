//go:build rest

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

package server

import (
	"context"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// attachHomeRealmResolver builds the home-realm discovery directory over the
// SHARED/system platform store (NOT the per-tenant routing store) and installs
// it on the REST config. Reading the shared store directly is what lets HRD
// resolve a login's tenant before that tenant's barrier unseals — there is no
// chicken-and-egg dependency on the very barrier the login is trying to reach.
//
// When the rest build tag is set this attaches a real resolver; the no-rest
// build provides a no-op variant.
func (s *Server) attachHomeRealmResolver(cfg *restapi.Config) {
	if dir := s.buildHomeRealmDirectory(); dir != nil {
		cfg.HomeRealmResolver = &homeRealmAdapter{dir: dir}
	}
}

// homeRealmAdapter bridges iam.HomeRealmResolver (which uses iam.RealmHints) to
// the rest package's resolver contract (rest.RealmHints), keeping the
// rest-only server.go free of an iam dependency.
type homeRealmAdapter struct {
	dir *iam.HomeRealmDirectory
}

func (a *homeRealmAdapter) Resolve(ctx context.Context, hints restapi.RealmHints) (string, error) {
	return a.dir.Resolve(ctx, iam.RealmHints{
		TenantHint: hints.TenantHint,
		SNIHost:    hints.SNIHost,
		Email:      hints.Email,
	})
}
