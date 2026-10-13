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

package fetcher_test

import (
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/fetcher"
)

// TestDefaultBundlePath_MatchesUpstream pins the alias to the upstream
// symbol, so a change of endpoint in go-xkms reaches consumers unchanged.
func TestDefaultBundlePath_MatchesUpstream(t *testing.T) {
	if fetcher.DefaultBundlePath != xkmssdk.TrustFetcherDefaultBundlePath {
		t.Fatalf("DefaultBundlePath = %q, want %q", fetcher.DefaultBundlePath, xkmssdk.TrustFetcherDefaultBundlePath)
	}
}

// TestDefaultTimeout_MatchesUpstream pins the timeout alias to upstream.
func TestDefaultTimeout_MatchesUpstream(t *testing.T) {
	if fetcher.DefaultTimeout != xkmssdk.TrustFetcherDefaultTimeout {
		t.Fatalf("DefaultTimeout = %v, want %v", fetcher.DefaultTimeout, xkmssdk.TrustFetcherDefaultTimeout)
	}
}
