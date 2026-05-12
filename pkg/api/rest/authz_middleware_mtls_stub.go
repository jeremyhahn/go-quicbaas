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

//go:build rest && !iam

package rest

import "net/http"

// enforceMTLSBinding is a no-op when the iam build tag is absent.
// mTLS proof-of-possession enforcement requires the iam build tag.
func enforceMTLSBinding(r *http.Request, rawToken string) error {
	return nil
}
