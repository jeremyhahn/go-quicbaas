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

// eab.go re-exports go-xkms's External Account Binding vocabulary
// (go-quicbaas#1090): the purpose a binding is minted for, its lifecycle
// status, and the device record a TPM-bound binding carries.
//
// go-trusted-platform writes its platform-issued EABs through to the ACME EAB
// store, so it must use go-xkms's values rather than copies: a copied
// "endorse-01" string drifts silently the day go-xkms renames it. Every name
// below is an alias of the go-xkms entity, so ACMEExternalAccountBinding
// values built with them are exactly what the ACME server reads.

package acme

import (
	acmeentities "github.com/jeremyhahn/go-xkms/pkg/acme/entities"
)

// EABDeviceInfo is the device record a TPM-bound External Account Binding
// carries (ACMEExternalAccountBinding.DeviceInfo).
type EABDeviceInfo = acmeentities.EABDeviceInfo

// The purposes an External Account Binding is minted for
// (ACMEExternalAccountBinding.Purpose).
const (
	// EABPurposeAccountBinding is standard RFC 8555 account binding.
	EABPurposeAccountBinding = acmeentities.EABPurposeAccountBinding

	// EABPurposeEndorse01 gates endorse-01 EK certificate issuance.
	EABPurposeEndorse01 = acmeentities.EABPurposeEndorse01

	// EABPurposeEnroll01 gates enroll-01 device enrollment.
	EABPurposeEnroll01 = acmeentities.EABPurposeEnroll01
)

// The lifecycle status of an External Account Binding
// (ACMEExternalAccountBinding.Status).
const (
	// EABStatusActive is a binding ready to be used.
	EABStatusActive = acmeentities.EABStatusActive

	// EABStatusUsed is a binding already consumed by an ACME account.
	EABStatusUsed = acmeentities.EABStatusUsed

	// EABStatusRevoked is a binding revoked before use.
	EABStatusRevoked = acmeentities.EABStatusRevoked

	// EABStatusExpired is a binding past its expiry.
	EABStatusExpired = acmeentities.EABStatusExpired
)
