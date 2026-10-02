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

package luks

import (
	"errors"
	"testing"

	xkmsluks "github.com/jeremyhahn/go-xkms/sdk/go/luks"
)

// The aliases must be the upstream types, not copies: a caller hands a
// TPMSealerConfig or SealParent built here straight to go-xkms.
var (
	_ xkmsluks.TPMSealerConfig = TPMSealerConfig{}
	_ xkmsluks.SealParent      = SealParentPSRK
	_ *xkmsluks.TPMSealer      = (*TPMSealer)(nil)
	_ xkmsluks.Executor        = Executor(nil)
)

func TestParseSealParent(t *testing.T) {
	cases := map[string]SealParent{"": SealParentPSRK, "psrk": SealParentPSRK, " SSRK ": SealParentSSRK}
	for in, want := range cases {
		got, err := ParseSealParent(in)
		if err != nil || got != want {
			t.Fatalf("ParseSealParent(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if SealParentPSRK.String() != "psrk" || SealParentSSRK.String() != "ssrk" {
		t.Fatalf("String() = %q, %q", SealParentPSRK, SealParentSSRK)
	}
}

func TestParseSealParent_RejectsUnknown(t *testing.T) {
	_, err := ParseSealParent("owner")
	if err == nil {
		t.Fatal("an unknown seal parent must be refused")
	}
	var invalid *SealParentInvalidError
	if !errors.As(err, &invalid) && !errors.Is(err, ErrInvalidSealParent) {
		t.Fatalf("error %v is neither SealParentInvalidError nor ErrInvalidSealParent", err)
	}
}

func TestTokenTypesDifferByParent(t *testing.T) {
	if TPMTokenType == XKMSTPMTokenType {
		t.Fatalf("SSRK and PSRK seals must write distinct token types, both are %q", TPMTokenType)
	}
}
