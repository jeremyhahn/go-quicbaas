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

// Package fetcher re-exports the defaults of go-xkms's SPKI-pinned CA-bundle
// fetcher, so a server that serves the bundle and a client that fetches it
// agree on the endpoint through the SDK path instead of each restating it.
//
// All symbols are pure aliases of the `TrustFetcher*` re-exports in
// `xkms/sdk/go`.
package fetcher

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

const (
	// DefaultBundlePath is the default CA-bundle endpoint path.
	DefaultBundlePath = xkmssdk.TrustFetcherDefaultBundlePath

	// DefaultTimeout is the HTTP timeout a fetch applies when the caller
	// sets none.
	DefaultTimeout = xkmssdk.TrustFetcherDefaultTimeout
)
