//go:build quic && !iam

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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// getIAMServiceOption returns a no-op handler option when IAM is not enabled.
func getIAMServiceOption(_ service.QuicBaaS) HandlerOption {
	return func(h *Handler) {}
}

// getMFAServiceOption returns a no-op handler option when IAM is not enabled.
func getMFAServiceOption(_ service.QuicBaaS) HandlerOption {
	return func(h *Handler) {}
}
