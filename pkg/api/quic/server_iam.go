//go:build quic

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

package quic

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// iamServiceProvider is an interface for services that can provide IAM service.
type iamServiceProvider interface {
	IAM() service.IAMService
}

// mfaManagerProvider is an interface for services that expose the MFA manager.
// The concrete quicBaaS type implements this when IAM is enabled.
type mfaManagerProvider interface {
	GetMFAManager(ctx context.Context) (service.MFAManager, error)
}

// getIAMServiceOption returns a handler option that injects the IAM service.
// This is called from handleStream when creating a new handler.
func getIAMServiceOption(svc service.QuicBaaS) HandlerOption {
	// Try to get IAM service from QuicBaaS
	if iamProvider, ok := svc.(iamServiceProvider); ok {
		iamSvc := iamProvider.IAM()
		if iamSvc != nil {
			return WithIAMService(iamSvc)
		}
	}
	// Return a no-op option if IAM is not available
	return func(h *Handler) {}
}

// getMFAServiceOption returns a handler option that injects the MFA service.
// The MFA manager is obtained from the QuicBaaS service which delegates to
// the IAM service provider when IAM is enabled. ctx must be the live request
// context so cancellation and deadline propagate into the manager lookup.
func getMFAServiceOption(ctx context.Context, svc service.QuicBaaS) HandlerOption {
	provider, ok := svc.(mfaManagerProvider)
	if !ok {
		return func(h *Handler) {}
	}

	mfaMgr, err := provider.GetMFAManager(ctx)
	if err != nil || mfaMgr == nil {
		return func(h *Handler) {}
	}

	return WithMFAService(mfaMgr)
}
