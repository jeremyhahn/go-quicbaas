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

// builder.go is the CA construction itself, expressed as plain
// error-returning functions so a caller that has no *testing.T can use it.
//
// The *testing.T helpers in xkmsca.go are thin wrappers over these. The split
// exists because the integration cert fixtures are issued by a program, not by
// a test: test/integration/certfixture builds the same CA this package builds
// for unit tests, over a filesystem directory instead of memory, so BOTH the
// unit suite and the integration cluster's TLS material come from one CA
// construction rather than two that can drift.

package xkmsca

import (
	"context"
	"errors"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// Default CA parameters. They are the values the *testing.T helpers have always
// used; naming them makes Config's zero value mean "the same CA as before".
const (
	// DefaultValidYears is the root certificate's lifetime in years.
	DefaultValidYears = 5

	// DefaultCertValidityDays is the lifetime, in days, of a leaf issued
	// without an explicit validity.
	DefaultCertValidityDays = 365

	// DefaultOrganization is the root subject's organization.
	DefaultOrganization = "Automate The Things"
)

// ErrNoCommonName is returned when a Config names no root common name. The CA
// addresses its own key and certificate by common name, so an empty one would
// produce a CA that cannot find its own material.
var ErrNoCommonName = errors.New("xkmsca: CA common name is required")

// Config describes the CA to build.
//
// The zero value plus a CommonName is the in-memory CA the unit suite uses.
type Config struct {
	// CommonName names the root. Required.
	CommonName string

	// Organization is the root subject's organization. Empty means
	// DefaultOrganization.
	Organization string

	// ValidYears is the root certificate's lifetime. Zero means
	// DefaultValidYears.
	ValidYears int

	// CertValidityDays is the default leaf lifetime. Zero means
	// DefaultCertValidityDays.
	CertValidityDays int

	// Dir is the filesystem root for the CA's key and certificate stores. Keys
	// live under Dir and certificates under Dir/certs, matching the layout the
	// daemon uses (pkg/server/lifecycle.go initializeCA).
	//
	// Empty keeps everything in memory: nothing is written, nothing survives
	// the process, and a unit test needs no cleanup. A non-empty Dir is what
	// makes a CA that a SECOND process can open — the integration fixtures
	// need that, because the material is issued by one program and extended by
	// the suite that runs against it.
	Dir string
}

// Build constructs the CA without initializing or loading it, so a caller can
// exercise the "the CA is not up" condition against a real CA rather than a
// stub that invents an error.
func Build(ctx context.Context, cfg Config) (xkmssdk.TrustedCA, error) {
	if cfg.CommonName == "" {
		return nil, ErrNoCommonName
	}

	keyStorage, certStorage, revocationStorage, err := storage(cfg.Dir)
	if err != nil {
		return nil, err
	}

	pkcs8Backend, err := xkmssdk.NewPKCS8Backend(&xkmssdk.PKCS8BackendConfig{
		KeyStorage:         keyStorage,
		AllowPlaintextKeys: true,
	})
	if err != nil {
		return nil, err
	}

	backend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     pkcs8Backend,
		CertStorage: certStorage,
	})
	if err != nil {
		return nil, err
	}

	certDAOs, err := xkmssdk.NewBackendDAOFactory(certStorage)
	if err != nil {
		return nil, err
	}
	certStore, err := xkmssdk.NewCertStore(&xkmssdk.CertStoreConfig{DAOs: certDAOs})
	if err != nil {
		return nil, err
	}

	identity := caIdentity(cfg)
	keyAttrs, err := identity.ToKeyAttributes(ctx)
	if err != nil {
		return nil, err
	}

	return xkmssdk.NewTrustedCA(&xkmssdk.TrustedCAParams{
		Config: &xkmssdk.TrustedCAConfig{
			Identity:                cfg.CommonName,
			Subject:                 &identity.Subject,
			ValidityDays:            identity.GetValidityDays(),
			IsRootCA:                true,
			MaxPathLength:           1,
			StoreType:               xkmssdk.StoreSoftware,
			DefaultCertValidityDays: validityDays(cfg),
		},
		KeyAttributes: keyAttrs,
		KeyStore:      backend,
		CertStore:     certStore,
		// One uniform software backend: every StoreType resolves to it, which
		// go-xkms requires be stated rather than assumed.
		BackendForStore: func(string, xkmssdk.StoreType) (xkmssdk.ServiceBackend, error) {
			return backend, nil
		},
		RevocationStore: revocationStorage,
	})
}

// Open builds the CA and brings it up, loading existing material when there is
// any and creating a root when there is not.
//
// Load-then-Init is the daemon's own order (pkg/server/lifecycle.go
// initializeCA). It matters for a CA with a Dir: a second Init over material
// that already exists would mint a SECOND root under the same subject name,
// and every leaf the first root signed would then fail verification against an
// authority the verifier can name — which reads as a broken cluster rather
// than as rotated trust.
func Open(ctx context.Context, cfg Config) (xkmssdk.TrustedCA, error) {
	ca, err := Build(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if loadErr := ca.Load(ctx); loadErr == nil {
		return ca, nil
	}
	if initErr := ca.Init(ctx); initErr != nil {
		return nil, initErr
	}
	return ca, nil
}

// storage returns the key, certificate and revocation storage backends for a
// Config's Dir: memory when it is empty, files under Dir, Dir/certs and
// Dir/revocations when it is not.
func storage(dir string) (keys, certs, revocations xkmssdk.StorageBackend, err error) {
	if dir == "" {
		return xkmssdk.NewStorageMemory(), xkmssdk.NewStorageMemory(), xkmssdk.NewStorageMemory(), nil
	}
	keys, err = xkmssdk.NewFileStorageBackend(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	certs, err = xkmssdk.NewFileStorageBackend(dir + "/certs")
	if err != nil {
		return nil, nil, nil, err
	}
	revocations, err = xkmssdk.NewFileStorageBackend(dir + "/revocations")
	if err != nil {
		return nil, nil, nil, err
	}
	return keys, certs, revocations, nil
}

// caIdentity builds the root identity from a Config, applying the defaults.
func caIdentity(cfg Config) xkmssdk.TrustedCAIdentity {
	org := cfg.Organization
	if org == "" {
		org = DefaultOrganization
	}
	years := cfg.ValidYears
	if years <= 0 {
		years = DefaultValidYears
	}
	return xkmssdk.TrustedCAIdentity{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   cfg.CommonName,
			Organization: org,
		},
		Valid: years,
		Keys: []*xkmssdk.KeyConfig{{
			KeyAlgorithm:       xkmssdk.AlgorithmECDSA,
			KeyType:            xkmssdk.KeyTypeStringCA,
			Hash:               xkmssdk.HashSHA256,
			SignatureAlgorithm: xkmssdk.SigECDSAWithSHA256,
			StoreType:          xkmssdk.StoreSoftware,
			ECCConfig:          &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP256},
		}},
		IsRoot: true,
	}
}

// validityDays returns the configured default leaf lifetime, or the package
// default when none is configured.
func validityDays(cfg Config) int {
	if cfg.CertValidityDays > 0 {
		return cfg.CertValidityDays
	}
	return DefaultCertValidityDays
}
