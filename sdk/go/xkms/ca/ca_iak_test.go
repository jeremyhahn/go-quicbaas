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

package ca_test

import (
	"testing"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/ca"
)

// Compile-time proof the IAK reader alias is the go-xkms interface.
var _ ca.CAIAKCertificateReader = xkmsacme.CAIAKCertificateReader(nil)

// TestCAIAKCommonName_IsTheUpstreamFunction pins the alias to go-xkms's
// IAKCommonName and to the documented "iak-<model>-<serial>" shape the CA
// stores the certificate under (go-xkms#371).
func TestCAIAKCommonName_IsTheUpstreamFunction(t *testing.T) {
	got := ca.CAIAKCommonName("SLB9665", "0x1234abcd")
	if want := xkmsacme.CAIAKCommonName("SLB9665", "0x1234abcd"); got != want {
		t.Fatalf("CAIAKCommonName = %q, upstream %q", got, want)
	}
	if got != "iak-SLB9665-0x1234abcd" {
		t.Fatalf("CAIAKCommonName = %q, want iak-<model>-<serial>", got)
	}
}
