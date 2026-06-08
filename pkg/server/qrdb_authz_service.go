//go:build iam

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

package server

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth/qrdbauthz"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// qrdbAuthzService bridges the qrdbauthz.Adapter to the
// interfaces.AuthorizationService seam shared by the QUIC and MCP transports.
// Each transport builds an AuthorizationRequest carrying the qrdb-canonical
// (resource, action) and the authenticated Subject; this bridge maps that onto
// the adapter's Subject and delegates the decision to qrdb's policy decision
// point. It is the single PEP -> PDP bridge for both transports and adds no
// policy of its own.
//
// Only Authorize is exercised by the handlers; the remaining
// AuthorizationService methods are part of the interface contract and surface
// the qrdb-backed posture (BatchAuthorize fans out to Authorize; there is no
// local provider to expose).
type qrdbAuthzService struct {
	adapter *qrdbauthz.Adapter
}

// Compile-time check that the bridge satisfies the authz service seam.
var _ interfaces.AuthorizationService = (*qrdbAuthzService)(nil)

// newQRDBAuthzService wraps a qrdbauthz.Adapter as an
// interfaces.AuthorizationService.
func newQRDBAuthzService(adapter *qrdbauthz.Adapter) *qrdbAuthzService {
	return &qrdbAuthzService{adapter: adapter}
}

// Authorize maps the authorization request onto the qrdbauthz subject and
// delegates to the qrdb-backed adapter. When the request subject carries no
// tenant it falls back to the cert-derived tenant on the context so the
// adapter's well-formed-identity invariant holds. A qrdb denial is surfaced as a
// not-allowed decision; the transport handler maps that to its permission-denied
// status.
func (s *qrdbAuthzService) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	tenantID := req.Subject.TenantID
	if tenantID == "" {
		tenantID = iam.TenantFromContext(ctx)
	}
	err := s.adapter.Authorize(ctx, qrdbauthz.Subject{
		ID:         req.Subject.ID,
		TenantID:   tenantID,
		Roles:      req.Subject.Roles,
		Attributes: req.Subject.Attributes,
	}, req.Resource, req.Action)
	if err != nil {
		return &interfaces.AuthorizationDecision{
			Allowed:  false,
			Reason:   err.Error(),
			Provider: "qrdb",
		}, nil
	}
	return &interfaces.AuthorizationDecision{Allowed: true, Provider: "qrdb"}, nil
}

// BatchAuthorize fans out to Authorize, returning one decision per request.
func (s *qrdbAuthzService) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))
	for i, req := range reqs {
		d, err := s.Authorize(ctx, req)
		if err != nil {
			return nil, err
		}
		decisions[i] = d
	}
	return decisions, nil
}

// GetProvider returns nil: the decision is made by qrdb's PDP, not by a local
// interfaces.AuthorizationProvider.
func (s *qrdbAuthzService) GetProvider() interfaces.AuthorizationProvider { return nil }

// GetProviderType identifies the qrdb-backed decision path.
func (s *qrdbAuthzService) GetProviderType() string { return "qrdb" }

// Close releases no resources: the adapter holds no closable state.
func (s *qrdbAuthzService) Close() error { return nil }
