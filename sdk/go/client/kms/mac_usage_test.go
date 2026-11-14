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

package kms

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The service has generate_mac and verify_mac usages for HMAC keys; the SDK
// had no way to ask for them, so an SDK caller's MAC key was made as an
// encrypt key and its MAC usage read back as ENCRYPT (#1782).
func TestKeyUsage_MACUsagesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		sdk  KeyUsage
		svc  service.KeyUsage
		name string
	}{
		{KeyUsageGenerateMAC, service.KeyUsageGenerateMAC, "GENERATE_MAC"},
		{KeyUsageVerifyMAC, service.KeyUsageVerifyMAC, "VERIFY_MAC"},
	} {
		if got := sdkUsageToServiceUsage(tc.sdk); got != tc.svc {
			t.Errorf("%s: to service = %q, want %q", tc.name, got, tc.svc)
		}
		if got := serviceUsageToSDKUsage(tc.svc); got != tc.sdk {
			t.Errorf("%s: from service = %v, want %v", tc.name, got, tc.sdk)
		}
		if got := tc.sdk.String(); got != tc.name {
			t.Errorf("String() = %q, want %q", got, tc.name)
		}
	}
}
