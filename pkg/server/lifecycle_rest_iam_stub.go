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

package server

import "crypto/tls"

// appendOperatorClientCAsFn is the seam that wires the operator's CA pool into
// the REST server's TLS ClientCAs when iam.mtls.tls_client_auth_ca_file is set.
// This stub is used when the iam build tag is absent; the real implementation
// lives in lifecycle_iam_mtls.go.
var appendOperatorClientCAsFn = func(_ *Server, _ *tls.Config) error {
	return nil
}
