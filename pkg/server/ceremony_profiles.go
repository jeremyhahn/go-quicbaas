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

package server

import (
	"crypto/x509"
	"sync"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// ceremonyProfileSpec describes a certificate profile the qrdb ceremony
// service invokes by name when signing officer / custodian CSRs. The set is
// derived from go-qrdb/pkg/ceremony/types.go (roleProfileMap) plus the
// literal "custodian" profile referenced by the M-of-N ceremony — the same
// canonical list xkmsd's init registers (cmd/xkmsd/init_ceremony.go).
type ceremonyProfileSpec struct {
	name         string
	keyUsage     x509.KeyUsage
	extKeyUsage  []x509.ExtKeyUsage
	validityDays int
}

// ceremonyProfileSpecs is the canonical role-profile list.
var ceremonyProfileSpecs = []ceremonyProfileSpec{
	{
		name:         "custodian",
		keyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		extKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		validityDays: 365,
	},
	{
		name:         "security-officer",
		keyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		extKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		validityDays: 365,
	},
	{
		name:         "administrator",
		keyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		extKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		validityDays: 365,
	},
	{
		name:         "operator",
		keyUsage:     x509.KeyUsageDigitalSignature,
		extKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		validityDays: 180,
	},
	{
		name:         "user",
		keyUsage:     x509.KeyUsageDigitalSignature,
		extKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		validityDays: 90,
	},
	{
		name:         "auditor",
		keyUsage:     x509.KeyUsageDigitalSignature,
		extKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		validityDays: 180,
	},
}

// ceremonyProfileMu serializes profile registration across concurrent
// ceremony CA adapter constructions.
var ceremonyProfileMu sync.Mutex

// profileRegistryProvider is the narrow slice of the concrete xkms CA that
// exposes its profile registry. The TrustedCA interface itself does not
// carry it; the concrete *ca.CA does.
type profileRegistryProvider interface {
	Profiles() xkmssdk.ProfileRegistry
}

// registerCeremonyProfiles installs the qrdb ceremony role profiles onto the
// server's xkms CA profile registry so ceremony CSR signing (officer
// registration, sign-csr) resolves its named profiles instead of failing
// with the CA's typed profile-not-found error. Register replaces on
// duplicate names, so repeated calls are idempotent. A CA implementation
// that exposes no profile registry is left untouched — its own typed
// profile-not-found error surfaces if a ceremony profile is ever needed.
func (s *Server) registerCeremonyProfiles() error {
	if s == nil || s.xkmsCA == nil {
		return nil
	}
	provider, ok := s.xkmsCA.(profileRegistryProvider)
	if !ok {
		return nil
	}
	registry := provider.Profiles()
	if registry == nil {
		return nil
	}
	ceremonyProfileMu.Lock()
	defer ceremonyProfileMu.Unlock()
	for _, spec := range ceremonyProfileSpecs {
		profile := xkmssdk.NewProfileBuilder(spec.name).
			WithKeyUsage(spec.keyUsage).
			WithExtKeyUsage(spec.extKeyUsage...).
			WithDefaultValidity(spec.validityDays).
			Build()
		if err := registry.Register(spec.name, profile); err != nil {
			return &CeremonyWiringError{Stage: "register ceremony profile " + spec.name, Cause: err}
		}
	}
	return nil
}
