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
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Barrier sub-opcode names
// =============================================================================

func TestBarrierSubOpcodeToString(t *testing.T) {
	cases := map[byte]string{
		SubOpBarrierStatus:             "BarrierStatus",
		SubOpBarrierInitialize:         "BarrierInitialize",
		SubOpBarrierUnseal:             "BarrierUnseal",
		SubOpBarrierSeal:               "BarrierSeal",
		SubOpBarrierShamirInitialize:   "BarrierShamirInitialize",
		SubOpBarrierShamirUnsealShare:  "BarrierShamirUnsealShare",
		SubOpBarrierShamirUnsealShares: "BarrierShamirUnsealShares",
		SubOpBarrierRekey:              "BarrierRekey",
		SubOpBarrierRecoveryGenerate:   "BarrierRecoveryGenerate",
		SubOpBarrierRecoveryRecover:    "BarrierRecoveryRecover",
		SubOpBarrierRecoveryDelete:     "BarrierRecoveryDelete",
		SubOpBarrierRecoveryStatus:     "BarrierRecoveryStatus",
		SubOpBarrierRootToken:          "BarrierRootToken",
	}
	for subOp, want := range cases {
		assert.Equal(t, want, BarrierSubOpcodeToString(subOp))
	}
	assert.Contains(t, BarrierSubOpcodeToString(0xFE), "UnknownBarrierOp")
}

// =============================================================================
// Ceremony sub-opcode names
// =============================================================================

func TestCeremonySubOpcodeToString(t *testing.T) {
	cases := map[byte]string{
		SubOpCeremonyInitialize:            "CeremonyInitialize",
		SubOpCeremonyStatus:                "CeremonyStatus",
		SubOpCeremonyClaimCertBegin:        "CeremonyClaimCertBegin",
		SubOpCeremonyClaimCertComplete:     "CeremonyClaimCertComplete",
		SubOpCeremonyClaimShare:            "CeremonyClaimShare",
		SubOpCeremonySignCSR:               "CeremonySignCSR",
		SubOpCeremonyListOfficers:          "CeremonyListOfficers",
		SubOpCeremonyEnrollExternalOfficer: "CeremonyEnrollExternalOfficer",
	}
	for subOp, want := range cases {
		assert.Equal(t, want, CeremonySubOpcodeToString(subOp))
	}
	assert.Contains(t, CeremonySubOpcodeToString(0xFE), "UnknownCeremonyOp")
}
