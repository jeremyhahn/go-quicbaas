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

package qrdbauthz

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
)

// DecisionPoint adapts the Adapter to the auth package's QRDBDecisionPoint
// seam, mapping auth.QRDBSubject onto the adapter's Subject. It is the single
// bridge between the in-process auth managers' narrow, iam-free seam and the
// iam-tagged qrdb authorizer, so the auth package carries no iam dependency.
type DecisionPoint struct {
	adapter *Adapter
}

// Compile-time check that DecisionPoint satisfies the auth seam.
var _ auth.QRDBDecisionPoint = (*DecisionPoint)(nil)

// NewDecisionPoint wraps an Adapter for use as an auth.QRDBDecisionPoint.
func NewDecisionPoint(adapter *Adapter) *DecisionPoint {
	return &DecisionPoint{adapter: adapter}
}

// Authorize maps the auth subject onto the adapter subject and delegates the
// decision to qrdb's policy decision point.
func (d *DecisionPoint) Authorize(ctx context.Context, subject auth.QRDBSubject, resource, action string) error {
	return d.adapter.Authorize(ctx, Subject{
		ID:       subject.ID,
		TenantID: subject.TenantID,
		Roles:    subject.Roles,
	}, resource, action)
}
